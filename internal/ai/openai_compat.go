package ai

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"net/http"
	"net/url"

	"github.com/nyelonong/basso/internal/suggest"
)

type OpenAICompatClient struct {
	config   Config
	client   *http.Client
	endpoint string
}

var _ suggest.Model = (*OpenAICompatClient)(nil)

func NewOpenAICompatClient(config Config, client *http.Client) *OpenAICompatClient {
	baseURL := ""
	if config.OpenAICompatBaseURL != nil {
		baseURL = config.OpenAICompatBaseURL.String()
	}
	return newOpenAICompatClient(config, client, baseURL)
}

func newOpenAICompatClient(config Config, client *http.Client, baseURL string) *OpenAICompatClient {
	endpoint := ""
	if baseURL != "" {
		if parsed, err := url.Parse(baseURL); err == nil {
			endpoint = parsed.JoinPath("chat/completions").String()
		}
	}
	return &OpenAICompatClient{
		config:   config,
		client:   redirectRejectingClient(client),
		endpoint: endpoint,
	}
}

func (client *OpenAICompatClient) Propose(
	ctx context.Context,
	request suggest.ModelRequest,
) (suggest.Proposal, error) {
	prompt, err := suggest.RenderPrompt(request)
	if err != nil {
		return suggest.Proposal{}, fmt.Errorf("openai-compatible: render prompt: %w", err)
	}

	payload := openAICompatRequest{
		Model: client.config.Model,
		Messages: []openAICompatMessage{
			{Role: "user", Content: prompt},
		},
	}
	body, err := json.Marshal(payload)
	if err != nil {
		return suggest.Proposal{}, fmt.Errorf("openai-compatible: encode request: %w", err)
	}

	requestContext, cancel := context.WithTimeout(ctx, client.config.Timeout)
	defer cancel()

	result, err := postProposal(requestContext, client.client, func(attemptCtx context.Context) (*http.Request, error) {
		httpRequest, err := http.NewRequestWithContext(
			attemptCtx,
			http.MethodPost,
			client.endpoint,
			bytes.NewReader(body),
		)
		if err != nil {
			return nil, err
		}
		httpRequest.Header.Set("Authorization", "Bearer "+client.config.OpenAICompatAPIKey)
		httpRequest.Header.Set("Content-Type", "application/json")
		return httpRequest, nil
	})
	if err != nil {
		return suggest.Proposal{}, fmt.Errorf("openai-compatible: %w", err)
	}
	if !result.ok() {
		return suggest.Proposal{}, fmt.Errorf("openai-compatible: unexpected HTTP status %d", result.statusCode)
	}
	responseBody := result.body

	var envelope openAICompatResponse
	if err := decodeJSON(responseBody, &envelope, false); err != nil {
		return suggest.Proposal{}, fmt.Errorf("openai-compatible: decode response: %w", err)
	}
	if len(envelope.Choices) != 1 {
		return suggest.Proposal{}, errors.New("openai-compatible: response must contain exactly one choice")
	}
	proposalContent := envelope.Choices[0].Message.Content
	if proposalContent == "" {
		return suggest.Proposal{}, errors.New("openai-compatible: response has no message content")
	}
	unwrapped, err := unwrapFencedProposal(proposalContent)
	if err != nil {
		return suggest.Proposal{}, fmt.Errorf("openai-compatible: invalid proposal: %w", err)
	}
	proposal, err := decodeProposal(unwrapped)
	if err != nil {
		return suggest.Proposal{}, fmt.Errorf("openai-compatible: invalid proposal: %w", err)
	}
	return proposal, nil
}

type openAICompatRequest struct {
	Model    string                `json:"model"`
	Messages []openAICompatMessage `json:"messages"`
}

type openAICompatMessage struct {
	Role    string `json:"role"`
	Content string `json:"content"`
}

type openAICompatResponse struct {
	Choices []openAICompatChoice `json:"choices"`
}

type openAICompatChoice struct {
	Message openAICompatMessage `json:"message"`
}
