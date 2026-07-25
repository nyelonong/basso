package ai

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"net/http"
	"strings"

	"github.com/nyelonong/basso/internal/suggest"
)

const (
	openAIOrigin        = "https://api.openai.com"
	maxResponseBodySize = 512 << 10
	maxSummarySize      = 500
	maxSourceSize       = 256 << 10
)

type OpenAIClient struct {
	config   Config
	client   *http.Client
	endpoint string
}

var _ suggest.Model = (*OpenAIClient)(nil)

func NewOpenAIClient(config Config, client *http.Client) *OpenAIClient {
	return newOpenAIClient(config, client, openAIOrigin)
}

func newOpenAIClient(config Config, client *http.Client, origin string) *OpenAIClient {
	return &OpenAIClient{
		config:   config,
		client:   redirectRejectingClient(client),
		endpoint: strings.TrimRight(origin, "/") + "/v1/responses",
	}
}

func (client *OpenAIClient) Propose(
	ctx context.Context,
	request suggest.ModelRequest,
) (suggest.Proposal, error) {
	prompt, err := suggest.RenderPrompt(request)
	if err != nil {
		return suggest.Proposal{}, fmt.Errorf("openai: render prompt: %w", err)
	}

	payload := openAIRequest{
		Model: client.config.Model,
		Input: prompt,
		Text: openAIText{
			Format: openAIFormat{
				Type:   "json_schema",
				Name:   "basso_proposal",
				Schema: proposalJSONSchema(),
				Strict: true,
			},
		},
	}
	body, err := json.Marshal(payload)
	if err != nil {
		return suggest.Proposal{}, fmt.Errorf("openai: encode request: %w", err)
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
		return suggest.Proposal{}, fmt.Errorf("openai: create request: %w", err)
	}
	httpRequest.Header.Set("Authorization", "Bearer "+client.config.OpenAIAPIKey)
	httpRequest.Header.Set("Content-Type", "application/json")

	response, err := client.client.Do(httpRequest)
	if err != nil {
		return suggest.Proposal{}, fmt.Errorf("openai: request failed: %w", err)
	}
	responseBody, err := readBoundedResponse(response)
	if err != nil {
		return suggest.Proposal{}, fmt.Errorf("openai: read response: %w", err)
	}
	if response.StatusCode < http.StatusOK || response.StatusCode >= http.StatusMultipleChoices {
		return suggest.Proposal{}, fmt.Errorf("openai: unexpected HTTP status %d", response.StatusCode)
	}

	var envelope openAIResponse
	if err := decodeJSON(responseBody, &envelope, false); err != nil {
		return suggest.Proposal{}, fmt.Errorf("openai: decode response: %w", err)
	}
	if envelope.Status != "completed" {
		return suggest.Proposal{}, errors.New("openai: response is incomplete")
	}

	outputText, err := openAIOutputText(envelope)
	if err != nil {
		return suggest.Proposal{}, err
	}
	proposal, err := decodeProposal(outputText)
	if err != nil {
		return suggest.Proposal{}, fmt.Errorf("openai: invalid proposal: %w", err)
	}
	return proposal, nil
}

type openAIRequest struct {
	Model string     `json:"model"`
	Input string     `json:"input"`
	Text  openAIText `json:"text"`
}

type openAIText struct {
	Format openAIFormat `json:"format"`
}

type openAIFormat struct {
	Type   string             `json:"type"`
	Name   string             `json:"name"`
	Schema proposalSchemaType `json:"schema"`
	Strict bool               `json:"strict"`
}

type proposalSchemaType struct {
	Type                 string                    `json:"type"`
	Properties           map[string]schemaProperty `json:"properties"`
	Required             []string                  `json:"required"`
	AdditionalProperties bool                      `json:"additionalProperties"`
}

type schemaProperty struct {
	Type string `json:"type"`
}

func proposalJSONSchema() proposalSchemaType {
	return proposalSchemaType{
		Type: "object",
		Properties: map[string]schemaProperty{
			"summary": {Type: "string"},
			"source":  {Type: "string"},
		},
		Required:             []string{"summary", "source"},
		AdditionalProperties: false,
	}
}

type openAIResponse struct {
	Status string         `json:"status"`
	Output []openAIOutput `json:"output"`
}

type openAIOutput struct {
	Type    string          `json:"type"`
	Role    string          `json:"role"`
	Content []openAIContent `json:"content"`
}

type openAIContent struct {
	Type    string `json:"type"`
	Text    string `json:"text"`
	Refusal string `json:"refusal"`
}

func openAIOutputText(response openAIResponse) (string, error) {
	var outputText string
	count := 0
	for _, output := range response.Output {
		if output.Type != "message" || output.Role != "assistant" {
			continue
		}
		for _, content := range output.Content {
			switch content.Type {
			case "refusal":
				return "", errors.New("openai: response refused")
			case "output_text":
				count++
				outputText = content.Text
			default:
				return "", errors.New("openai: unexpected assistant content")
			}
		}
	}
	if count != 1 {
		return "", errors.New("openai: response must contain exactly one output_text item")
	}
	return outputText, nil
}

func redirectRejectingClient(client *http.Client) *http.Client {
	if client == nil {
		client = http.DefaultClient
	}
	cloned := *client
	cloned.CheckRedirect = func(*http.Request, []*http.Request) error {
		return errors.New("redirects are not allowed")
	}
	return &cloned
}

func readBoundedResponse(response *http.Response) ([]byte, error) {
	body, readErr := io.ReadAll(io.LimitReader(response.Body, maxResponseBodySize+1))
	closeErr := response.Body.Close()
	if readErr != nil {
		return nil, readErr
	}
	if closeErr != nil {
		return nil, closeErr
	}
	if len(body) > maxResponseBodySize {
		return nil, errors.New("response body exceeds 512 KiB")
	}
	return body, nil
}

func decodeJSON(data []byte, destination any, strict bool) error {
	decoder := json.NewDecoder(bytes.NewReader(data))
	if strict {
		decoder.DisallowUnknownFields()
	}
	if err := decoder.Decode(destination); err != nil {
		return err
	}
	if err := decoder.Decode(&struct{}{}); !errors.Is(err, io.EOF) {
		if err == nil {
			return errors.New("multiple JSON values")
		}
		return err
	}
	return nil
}

func decodeProposal(data string) (suggest.Proposal, error) {
	decoder := json.NewDecoder(strings.NewReader(data))
	token, err := decoder.Token()
	if err != nil {
		return suggest.Proposal{}, err
	}
	if delimiter, ok := token.(json.Delim); !ok || delimiter != '{' {
		return suggest.Proposal{}, errors.New("proposal must be an object")
	}

	var proposal suggest.Proposal
	var hasSummary bool
	var hasSource bool
	for decoder.More() {
		keyToken, err := decoder.Token()
		if err != nil {
			return suggest.Proposal{}, err
		}
		key, ok := keyToken.(string)
		if !ok {
			return suggest.Proposal{}, errors.New("proposal field name must be a string")
		}
		switch key {
		case "summary":
			if hasSummary {
				return suggest.Proposal{}, errors.New("proposal contains duplicate summary")
			}
			hasSummary = true
			if err := decoder.Decode(&proposal.Summary); err != nil {
				return suggest.Proposal{}, errors.New("summary must be a string")
			}
		case "source":
			if hasSource {
				return suggest.Proposal{}, errors.New("proposal contains duplicate source")
			}
			hasSource = true
			if err := decoder.Decode(&proposal.Source); err != nil {
				return suggest.Proposal{}, errors.New("source must be a string")
			}
		default:
			return suggest.Proposal{}, fmt.Errorf("proposal contains unexpected field %q", key)
		}
	}
	if _, err := decoder.Token(); err != nil {
		return suggest.Proposal{}, err
	}
	if err := decoder.Decode(&struct{}{}); !errors.Is(err, io.EOF) {
		if err == nil {
			return suggest.Proposal{}, errors.New("multiple JSON values")
		}
		return suggest.Proposal{}, err
	}
	if !hasSummary || !hasSource {
		return suggest.Proposal{}, errors.New("proposal must contain exactly summary and source")
	}
	if strings.TrimSpace(proposal.Summary) == "" {
		return suggest.Proposal{}, errors.New("summary is empty")
	}
	if len(proposal.Summary) > maxSummarySize {
		return suggest.Proposal{}, errors.New("summary exceeds 500 bytes")
	}
	if strings.TrimSpace(proposal.Source) == "" {
		return suggest.Proposal{}, errors.New("source is empty")
	}
	if len(proposal.Source) > maxSourceSize {
		return suggest.Proposal{}, errors.New("source exceeds 256 KiB")
	}
	return proposal, nil
}
