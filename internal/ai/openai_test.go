package ai

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"net/http"
	"net/http/httptest"
	"net/url"
	"reflect"
	"strings"
	"testing"
	"time"

	"github.com/nyelonong/basso/internal/suggest"
)

func TestOpenAIClient_SendsStrictSchemaRequest(t *testing.T) {
	request := testModelRequest()
	renderedPrompt, err := suggest.RenderPrompt(request)
	if err != nil {
		t.Fatalf("RenderPrompt() error = %v", err)
	}

	server := httptest.NewServer(http.HandlerFunc(func(response http.ResponseWriter, incoming *http.Request) {
		if incoming.Method != http.MethodPost {
			t.Errorf("method = %q, want %q", incoming.Method, http.MethodPost)
		}
		if incoming.URL.Path != "/v1/responses" {
			t.Errorf("path = %q, want %q", incoming.URL.Path, "/v1/responses")
		}
		if got := incoming.Header.Get("Authorization"); got != "Bearer test-api-key" {
			t.Errorf("Authorization = %q, want bearer API key", got)
		}
		if got := incoming.Header.Get("Content-Type"); got != "application/json" {
			t.Errorf("Content-Type = %q, want %q", got, "application/json")
		}

		var got map[string]any
		decoder := json.NewDecoder(incoming.Body)
		if err := decoder.Decode(&got); err != nil {
			t.Errorf("decode request: %v", err)
		}
		want := map[string]any{
			"model": "test-model",
			"input": renderedPrompt,
			"text": map[string]any{
				"format": map[string]any{
					"type":   "json_schema",
					"name":   "basso_proposal",
					"strict": true,
					"schema": proposalSchemaForTest(),
				},
			},
		}
		if !reflect.DeepEqual(got, want) {
			t.Errorf("request body = %#v, want %#v", got, want)
		}

		writeResponseForTest(t, response, `{
			"status":"completed",
			"output":[{
				"type":"message",
				"role":"assistant",
				"content":[{"type":"output_text","text":"{\"summary\":\"Denser hats\",\"source\":\"pattern\"}"}]
			}]
		}`)
	}))
	defer server.Close()

	client := newOpenAIClient(
		Config{
			Model:        "test-model",
			Timeout:      time.Second,
			OpenAIAPIKey: "test-api-key",
		},
		server.Client(),
		server.URL,
	)
	if _, err := client.Propose(context.Background(), request); err != nil {
		t.Fatalf("Propose() error = %v", err)
	}
}

func TestOpenAIClient_ParsesProposal(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(response http.ResponseWriter, _ *http.Request) {
		writeResponseForTest(t, response, `{
			"status":"completed",
			"output":[
				{"type":"reasoning","summary":[]},
				{
					"type":"message",
					"role":"assistant",
					"content":[{
						"type":"output_text",
						"text":"{\"summary\":\"Add a brass response\",\"source\":\"(fn pattern [bar] [])\\npattern\"}",
						"annotations":[]
					}]
				}
			]
		}`)
	}))
	defer server.Close()

	client := newOpenAIClient(
		Config{
			Model:        "test-model",
			Timeout:      time.Second,
			OpenAIAPIKey: "test-api-key",
		},
		server.Client(),
		server.URL,
	)
	got, err := client.Propose(context.Background(), testModelRequest())
	if err != nil {
		t.Fatalf("Propose() error = %v", err)
	}
	want := suggest.Proposal{
		Summary: "Add a brass response",
		Source:  "(fn pattern [bar] [])\npattern",
	}
	if got != want {
		t.Errorf("Propose() = %#v, want %#v", got, want)
	}
}

func TestClients_RejectRefusalMalformedTruncatedAndOversizedResponses(t *testing.T) {
	oversizedSummary := strings.Repeat("s", maxSummarySize+1)
	oversizedSource := strings.Repeat("x", maxSourceSize+1)
	tests := []struct {
		name       string
		provider   string
		statusCode int
		body       string
	}{
		{
			name:     "OpenAI refusal",
			provider: "openai",
			body: `{"status":"completed","output":[{
				"type":"message","role":"assistant",
				"content":[{"type":"refusal","refusal":"private provider body"}]
			}]}`,
		},
		{
			name:     "OpenAI incomplete",
			provider: "openai",
			body:     `{"status":"incomplete","output":[]}`,
		},
		{
			name:     "OpenAI missing status",
			provider: "openai",
			body: `{"output":[{
				"type":"message","role":"assistant",
				"content":[{"type":"output_text","text":"{\"summary\":\"change\",\"source\":\"pattern\"}"}]
			}]}`,
		},
		{
			name:     "OpenAI empty status",
			provider: "openai",
			body: `{"status":"","output":[{
				"type":"message","role":"assistant",
				"content":[{"type":"output_text","text":"{\"summary\":\"change\",\"source\":\"pattern\"}"}]
			}]}`,
		},
		{
			name:     "OpenAI malformed envelope",
			provider: "openai",
			body:     `{"status":`,
		},
		{
			name:     "OpenAI truncated proposal",
			provider: "openai",
			body:     openAIResponseForTest(`{"summary":"change"`),
		},
		{
			name:     "OpenAI duplicate proposal field",
			provider: "openai",
			body:     openAIResponseForTest(`{"summary":"first","summary":"second","source":"pattern"}`),
		},
		{
			name:     "OpenAI extra proposal field",
			provider: "openai",
			body:     openAIResponseForTest(`{"summary":"change","source":"pattern","extra":"field"}`),
		},
		{
			name:     "OpenAI empty proposal field",
			provider: "openai",
			body:     openAIResponseForTest(`{"summary":" ","source":"pattern"}`),
		},
		{
			name:     "OpenAI oversized summary",
			provider: "openai",
			body:     openAIResponseForTest(fmt.Sprintf(`{"summary":%q,"source":"pattern"}`, oversizedSummary)),
		},
		{
			name:     "OpenAI oversized source",
			provider: "openai",
			body:     openAIResponseForTest(fmt.Sprintf(`{"summary":"change","source":%q}`, oversizedSource)),
		},
		{
			name:     "OpenAI oversized body",
			provider: "openai",
			body:     strings.Repeat("x", maxResponseBodySize+1),
		},
		{
			name:       "OpenAI non-2xx",
			provider:   "openai",
			statusCode: http.StatusTooManyRequests,
			body:       "private provider body with test-api-key and Authorization",
		},
		{
			name:     "Ollama incomplete",
			provider: "ollama",
			body:     `{"done":false,"message":{"role":"assistant","content":"private provider body"}}`,
		},
		{
			name:     "Ollama malformed envelope",
			provider: "ollama",
			body:     `{"done":`,
		},
		{
			name:     "Ollama truncated proposal",
			provider: "ollama",
			body:     ollamaResponseForTest(`{"summary":"change"`),
		},
		{
			name:     "Ollama duplicate proposal field",
			provider: "ollama",
			body:     ollamaResponseForTest(`{"summary":"first","summary":"second","source":"pattern"}`),
		},
		{
			name:     "Ollama extra proposal field",
			provider: "ollama",
			body:     ollamaResponseForTest(`{"summary":"change","source":"pattern","extra":"field"}`),
		},
		{
			name:     "Ollama empty proposal field",
			provider: "ollama",
			body:     ollamaResponseForTest(`{"summary":"change","source":" "}`),
		},
		{
			name:     "Ollama oversized summary",
			provider: "ollama",
			body:     ollamaResponseForTest(fmt.Sprintf(`{"summary":%q,"source":"pattern"}`, oversizedSummary)),
		},
		{
			name:     "Ollama oversized source",
			provider: "ollama",
			body:     ollamaResponseForTest(fmt.Sprintf(`{"summary":"change","source":%q}`, oversizedSource)),
		},
		{
			name:     "Ollama oversized body",
			provider: "ollama",
			body:     strings.Repeat("x", maxResponseBodySize+1),
		},
		{
			name:       "Ollama non-2xx",
			provider:   "ollama",
			statusCode: http.StatusInternalServerError,
			body:       "private provider body with test-api-key and Authorization",
		},
		{
			name:     "Compatible empty choices",
			provider: "openai-compatible",
			body:     `{"choices":[]}`,
		},
		{
			name:     "Compatible multiple choices",
			provider: "openai-compatible",
			body:     `{"choices":[{"message":{"role":"assistant","content":"{}"}},{"message":{"role":"assistant","content":"{}"}}]}`,
		},
		{
			name:     "Compatible missing message content",
			provider: "openai-compatible",
			body:     `{"choices":[{"message":{"role":"assistant"}}]}`,
		},
		{
			name:     "Compatible malformed envelope",
			provider: "openai-compatible",
			body:     `{"choices":`,
		},
		{
			name:     "Compatible truncated proposal",
			provider: "openai-compatible",
			body:     openAICompatResponseForTest(`{"summary":"change"`),
		},
		{
			name:       "Compatible non-2xx",
			provider:   "openai-compatible",
			statusCode: http.StatusUnauthorized,
			body:       "private provider body with test-api-key and Authorization",
		},
		{
			name:     "Compatible oversized body",
			provider: "openai-compatible",
			body:     strings.Repeat("x", maxResponseBodySize+1),
		},
	}

	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			server := httptest.NewServer(http.HandlerFunc(func(response http.ResponseWriter, _ *http.Request) {
				statusCode := test.statusCode
				if statusCode == 0 {
					statusCode = http.StatusOK
				}
				response.WriteHeader(statusCode)
				writeResponseForTest(t, response, test.body)
			}))
			defer server.Close()

			client := modelClientForTest(t, test.provider, server)
			_, err := client.Propose(context.Background(), testModelRequest())
			if err == nil {
				t.Fatal("Propose() error = nil, want error")
			}
			for _, secret := range []string{
				"test-api-key",
				"Authorization",
				"private provider body",
			} {
				if strings.Contains(err.Error(), secret) {
					t.Errorf("Propose() error contains secret provider data %q: %v", secret, err)
				}
			}
		})
	}
}

func TestClients_RefuseRedirects(t *testing.T) {
	for _, provider := range []string{"openai", "ollama", "openai-compatible"} {
		t.Run(provider, func(t *testing.T) {
			redirectedHeaders := make(chan http.Header, 1)
			target := httptest.NewServer(http.HandlerFunc(func(response http.ResponseWriter, incoming *http.Request) {
				redirectedHeaders <- incoming.Header.Clone()
				if provider == "openai" {
					writeResponseForTest(t, response, openAIResponseForTest(
						`{"summary":"change","source":"pattern"}`,
					))
					return
				}
				writeResponseForTest(t, response, ollamaResponseForTest(
					`{"summary":"change","source":"pattern"}`,
				))
			}))
			defer target.Close()

			redirector := httptest.NewServer(http.HandlerFunc(func(response http.ResponseWriter, incoming *http.Request) {
				if provider == "openai" && incoming.Header.Get("Authorization") != "Bearer test-api-key" {
					t.Errorf("initial Authorization = %q, want bearer API key", incoming.Header.Get("Authorization"))
				}
				http.Redirect(response, incoming, target.URL, http.StatusTemporaryRedirect)
			}))
			defer redirector.Close()

			client := modelClientForTest(t, provider, redirector)
			_, err := client.Propose(context.Background(), testModelRequest())
			if err == nil {
				t.Fatal("Propose() error = nil, want redirect error")
			}
			if strings.Contains(err.Error(), "test-api-key") ||
				strings.Contains(err.Error(), "Authorization") {
				t.Errorf("Propose() error contains credential data: %v", err)
			}

			select {
			case headers := <-redirectedHeaders:
				t.Errorf("redirect target was contacted with headers %#v", headers)
			default:
			}
		})
	}
}

func TestClients_RespectContextAndTimeout(t *testing.T) {
	for _, provider := range []string{"openai", "ollama", "openai-compatible"} {
		t.Run(provider+" caller cancellation", func(t *testing.T) {
			server := httptest.NewServer(http.HandlerFunc(func(response http.ResponseWriter, _ *http.Request) {
				t.Error("provider was contacted for an already-canceled context")
				response.WriteHeader(http.StatusInternalServerError)
			}))
			defer server.Close()

			client := modelClientForTest(t, provider, server)
			ctx, cancel := context.WithCancel(context.Background())
			cancel()

			_, err := client.Propose(ctx, testModelRequest())
			if !errors.Is(err, context.Canceled) {
				t.Errorf("Propose() error = %v, want context.Canceled", err)
			}
		})

		t.Run(provider+" configured timeout", func(t *testing.T) {
			releaseHandler := make(chan struct{})
			server := httptest.NewServer(http.HandlerFunc(func(_ http.ResponseWriter, _ *http.Request) {
				<-releaseHandler
			}))
			defer server.Close()

			client := modelClientForTestWithTimeout(t, provider, server, 20*time.Millisecond)
			parent, cancel := context.WithTimeout(context.Background(), 250*time.Millisecond)
			defer cancel()

			started := time.Now()
			_, err := client.Propose(parent, testModelRequest())
			elapsed := time.Since(started)
			close(releaseHandler)
			if !errors.Is(err, context.DeadlineExceeded) {
				t.Errorf("Propose() error = %v, want context.DeadlineExceeded", err)
			}
			if elapsed >= 150*time.Millisecond {
				t.Errorf("Propose() elapsed = %v, want configured timeout before parent deadline", elapsed)
			}
		})
	}
}

func testModelRequest() suggest.ModelRequest {
	return suggest.ModelRequest{
		Prompt:  "Make the hats denser.",
		Source:  "pattern",
		Samples: []string{"kick.wav", "hat.wav"},
		Instruments: []suggest.Instrument{{
			Name: "bass", Description: "low voice", RecommendedRange: "C1-C3",
		}},
	}
}

func modelClientForTest(t *testing.T, provider string, server *httptest.Server) suggest.Model {
	t.Helper()

	return modelClientForTestWithTimeout(t, provider, server, time.Second)
}

func modelClientForTestWithTimeout(
	t *testing.T,
	provider string,
	server *httptest.Server,
	timeout time.Duration,
) suggest.Model {
	t.Helper()

	config := Config{
		Model:        "test-model",
		Timeout:      timeout,
		OpenAIAPIKey: "test-api-key",
	}
	switch provider {
	case "openai":
		return newOpenAIClient(config, server.Client(), server.URL)
	case "ollama":
		origin, err := url.Parse(server.URL)
		if err != nil {
			t.Fatalf("parse server URL: %v", err)
		}
		config.OllamaURL = origin
		return NewOllamaClient(config, server.Client())
	case "openai-compatible":
		config.OpenAICompatAPIKey = "test-api-key"
		return newOpenAICompatClient(config, server.Client(), server.URL+"/zen/go/v1")
	default:
		t.Fatalf("unsupported test provider %q", provider)
		return nil
	}
}

func openAIResponseForTest(proposal string) string {
	return fmt.Sprintf(`{
		"status":"completed",
		"output":[{
			"type":"message",
			"role":"assistant",
			"content":[{"type":"output_text","text":%q}]
		}]
	}`, proposal)
}

func ollamaResponseForTest(proposal string) string {
	return fmt.Sprintf(`{
		"done":true,
		"message":{"role":"assistant","content":%q}
	}`, proposal)
}

func writeResponseForTest(t *testing.T, response http.ResponseWriter, body string) {
	t.Helper()

	response.Header().Set("Content-Type", "application/json")
	if _, err := response.Write([]byte(body)); err != nil {
		t.Errorf("write response: %v", err)
	}
}

func proposalSchemaForTest() map[string]any {
	return map[string]any{
		"type": "object",
		"properties": map[string]any{
			"summary": map[string]any{"type": "string"},
			"source":  map[string]any{"type": "string"},
		},
		"required":             []any{"summary", "source"},
		"additionalProperties": false,
	}
}
