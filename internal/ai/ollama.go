package ai

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"net/http"

	"github.com/nyelonong/basso/internal/suggest"
)

type OllamaClient struct {
	config   Config
	client   *http.Client
	endpoint string
}

var _ suggest.Model = (*OllamaClient)(nil)

func NewOllamaClient(config Config, client *http.Client) *OllamaClient {
	endpoint := ""
	if config.OllamaURL != nil {
		urlCopy := *config.OllamaURL
		urlCopy.Path = "/api/chat"
		urlCopy.RawPath = ""
		endpoint = urlCopy.String()
	}
	return &OllamaClient{
		config:   config,
		client:   redirectRejectingClient(client),
		endpoint: endpoint,
	}
}

func (client *OllamaClient) Propose(
	ctx context.Context,
	request suggest.ModelRequest,
) (suggest.Proposal, error) {
	prompt, err := suggest.RenderPrompt(request)
	if err != nil {
		return suggest.Proposal{}, fmt.Errorf("ollama: render prompt: %w", err)
	}

	payload := ollamaRequest{
		Model: client.config.Model,
		Messages: []ollamaMessage{
			{Role: "user", Content: prompt},
		},
		Stream: false,
		Format: proposalJSONSchema(),
	}
	body, err := json.Marshal(payload)
	if err != nil {
		return suggest.Proposal{}, fmt.Errorf("ollama: encode request: %w", err)
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
		return suggest.Proposal{}, fmt.Errorf("ollama: create request: %w", err)
	}
	httpRequest.Header.Set("Content-Type", "application/json")

	response, err := client.client.Do(httpRequest)
	if err != nil {
		return suggest.Proposal{}, fmt.Errorf("ollama: request failed: %w", err)
	}
	responseBody, err := readBoundedResponse(response)
	if err != nil {
		return suggest.Proposal{}, fmt.Errorf("ollama: read response: %w", err)
	}
	if response.StatusCode < http.StatusOK || response.StatusCode >= http.StatusMultipleChoices {
		return suggest.Proposal{}, fmt.Errorf("ollama: unexpected HTTP status %d", response.StatusCode)
	}

	var envelope ollamaResponse
	if err := decodeJSON(responseBody, &envelope, false); err != nil {
		return suggest.Proposal{}, fmt.Errorf("ollama: decode response: %w", err)
	}
	if !envelope.Done {
		return suggest.Proposal{}, errors.New("ollama: response is incomplete")
	}
	if envelope.Message.Role != "assistant" {
		return suggest.Proposal{}, errors.New("ollama: response has no assistant message")
	}
	proposal, err := decodeProposal(envelope.Message.Content)
	if err != nil {
		return suggest.Proposal{}, fmt.Errorf("ollama: invalid proposal: %w", err)
	}
	return proposal, nil
}

type ollamaRequest struct {
	Model    string             `json:"model"`
	Messages []ollamaMessage    `json:"messages"`
	Stream   bool               `json:"stream"`
	Format   proposalSchemaType `json:"format"`
}

type ollamaMessage struct {
	Role    string `json:"role"`
	Content string `json:"content"`
}

type ollamaResponse struct {
	Done    bool          `json:"done"`
	Message ollamaMessage `json:"message"`
}
