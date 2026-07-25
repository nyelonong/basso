package ai

import (
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"net/url"
	"reflect"
	"testing"
	"time"

	"github.com/nyelonong/basso/internal/suggest"
)

func TestOllamaClient_SendsStrictSchemaRequest(t *testing.T) {
	request := testModelRequest()
	renderedPrompt, err := suggest.RenderPrompt(request)
	if err != nil {
		t.Fatalf("RenderPrompt() error = %v", err)
	}

	server := httptest.NewServer(http.HandlerFunc(func(response http.ResponseWriter, incoming *http.Request) {
		if incoming.Method != http.MethodPost {
			t.Errorf("method = %q, want %q", incoming.Method, http.MethodPost)
		}
		if incoming.URL.Path != "/api/chat" {
			t.Errorf("path = %q, want %q", incoming.URL.Path, "/api/chat")
		}
		if got := incoming.Header.Get("Authorization"); got != "" {
			t.Errorf("Authorization = %q, want empty", got)
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
			"messages": []any{
				map[string]any{
					"role":    "user",
					"content": renderedPrompt,
				},
			},
			"stream": false,
			"format": proposalSchemaForTest(),
		}
		if !reflect.DeepEqual(got, want) {
			t.Errorf("request body = %#v, want %#v", got, want)
		}

		writeResponseForTest(t, response, `{
			"done":true,
			"message":{
				"role":"assistant",
				"content":"{\"summary\":\"Denser hats\",\"source\":\"pattern\"}"
			}
		}`)
	}))
	defer server.Close()

	origin, err := url.Parse(server.URL)
	if err != nil {
		t.Fatalf("parse server URL: %v", err)
	}
	client := NewOllamaClient(
		Config{
			Model:     "test-model",
			Timeout:   time.Second,
			OllamaURL: origin,
		},
		server.Client(),
	)
	if _, err := client.Propose(context.Background(), request); err != nil {
		t.Fatalf("Propose() error = %v", err)
	}
}

func TestOllamaClient_ParsesProposal(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(response http.ResponseWriter, _ *http.Request) {
		writeResponseForTest(t, response, `{
			"done":true,
			"message":{
				"role":"assistant",
				"content":"{\"summary\":\"Add a bass response\",\"source\":\"(fn pattern [bar] [])\\npattern\"}"
			}
		}`)
	}))
	defer server.Close()

	origin, err := url.Parse(server.URL)
	if err != nil {
		t.Fatalf("parse server URL: %v", err)
	}
	client := NewOllamaClient(
		Config{
			Model:     "test-model",
			Timeout:   time.Second,
			OllamaURL: origin,
		},
		server.Client(),
	)
	got, err := client.Propose(context.Background(), testModelRequest())
	if err != nil {
		t.Fatalf("Propose() error = %v", err)
	}
	want := suggest.Proposal{
		Summary: "Add a bass response",
		Source:  "(fn pattern [bar] [])\npattern",
	}
	if got != want {
		t.Errorf("Propose() = %#v, want %#v", got, want)
	}
}

func TestOllamaClient_ParsesMarkdownFencedProposal(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(response http.ResponseWriter, _ *http.Request) {
		writeResponseForTest(t, response, `{
			"done":true,
			"message":{
				"role":"assistant",
				"content":"`+"```json\\n{\\\"summary\\\":\\\"Denser hats\\\",\\\"source\\\":\\\"(fn pattern [bar] [])\\\\npattern\\\"}\\n```"+`"
			}
		}`)
	}))
	defer server.Close()

	origin, err := url.Parse(server.URL)
	if err != nil {
		t.Fatalf("parse server URL: %v", err)
	}
	client := NewOllamaClient(
		Config{
			Model:     "test-model",
			Timeout:   time.Second,
			OllamaURL: origin,
		},
		server.Client(),
	)

	got, err := client.Propose(context.Background(), testModelRequest())
	if err != nil {
		t.Fatalf("Propose() error = %v", err)
	}
	want := suggest.Proposal{
		Summary: "Denser hats",
		Source:  "(fn pattern [bar] [])\npattern",
	}
	if got != want {
		t.Errorf("Propose() = %#v, want %#v", got, want)
	}
}

func TestUnwrapOllamaProposal_RejectsAmbiguousMarkdown(t *testing.T) {
	tests := []struct {
		name    string
		content string
	}{
		{
			name:    "incomplete fence",
			content: "```json\n{}",
		},
		{
			name:    "unsupported fence label",
			content: "```fennel\n{}\n```",
		},
		{
			name:    "prose after fence",
			content: "```json\n{}\n```\nThis is the proposal.",
		},
		{
			name:    "empty fence",
			content: "```json\n\n```",
		},
	}

	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			if _, err := unwrapOllamaProposal(test.content); err == nil {
				t.Fatal("unwrapOllamaProposal() error = nil, want rejection")
			}
		})
	}
}
