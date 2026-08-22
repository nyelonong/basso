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

	httpRequest, err := http.NewRequestWithContext(
		requestContext,
		http.MethodPost,
		client.endpoint,
		bytes.NewReader(body),
	)
	if err != nil {
		return suggest.Proposal{}, fmt.Errorf("openai-compatible: create request: %w", err)
	}
	httpRequest.Header.Set("Authorization", "Bearer "+client.config.OpenAICompatAPIKey)
	httpRequest.Header.Set("Content-Type", "application/json")

	response, err := client.client.Do(httpRequest)
	if err != nil {
		return suggest.Proposal{}, fmt.Errorf("openai-compatible: request failed: %w", err)
	}
	responseBody, err := readBoundedResponse(response)
	if err != nil {
		return suggest.Proposal{}, fmt.Errorf("openai-compatible: read response: %w", err)
	}
	if response.StatusCode < http.StatusOK || response.StatusCode >= http.StatusMultipleChoices {
		return suggest.Proposal{}, fmt.Errorf("openai-compatible: unexpected HTTP status %d", response.StatusCode)
	}

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
