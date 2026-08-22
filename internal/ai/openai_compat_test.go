package ai

import (
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"reflect"
	"testing"
	"time"

	"github.com/nyelonong/basso/internal/suggest"
)

func TestOpenAICompatClient_SendsMinimalChatRequest(t *testing.T) {
	request := testModelRequest()
	renderedPrompt, err := suggest.RenderPrompt(request)
	if err != nil {
		t.Fatalf("RenderPrompt() error = %v", err)
	}

	server := httptest.NewServer(http.HandlerFunc(func(response http.ResponseWriter, incoming *http.Request) {
		if incoming.Method != http.MethodPost {
			t.Errorf("method = %q, want %q", incoming.Method, http.MethodPost)
		}
		if incoming.URL.Path != "/zen/go/v1/chat/completions" {
			t.Errorf("path = %q, want %q", incoming.URL.Path, "/zen/go/v1/chat/completions")
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
			"messages": []any{
				map[string]any{"role": "user", "content": renderedPrompt},
			},
		}
		if !reflect.DeepEqual(got, want) {
			t.Errorf("request body = %#v, want %#v", got, want)
		}

		writeResponseForTest(
			t,
			response,
			openAICompatResponseForTest(`{"summary":"Denser hats","source":"pattern"}`),
		)
	}))
	defer server.Close()

	client := newOpenAICompatClient(
		Config{
			Model:              "test-model",
			Timeout:            time.Second,
			OpenAICompatAPIKey: "test-api-key",
		},
		server.Client(),
		server.URL+"/zen/go/v1",
	)
	proposal, err := client.Propose(context.Background(), request)
	if err != nil {
		t.Fatalf("Propose() error = %v", err)
	}
	if proposal.Summary != "Denser hats" || proposal.Source != "pattern" {
		t.Errorf("proposal = %#v, want parsed summary and source", proposal)
	}
}

func TestOpenAICompatClient_ParsesFencedProposal(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(response http.ResponseWriter, _ *http.Request) {
		writeResponseForTest(
			t,
			response,
			openAICompatResponseForTest("```fennel\n{\"summary\":\"Add a brass response\",\"source\":\"pattern\"}\n```"),
		)
	}))
	defer server.Close()

	client := newOpenAICompatClient(
		Config{
			Model:              "test-model",
			Timeout:            time.Second,
			OpenAICompatAPIKey: "test-api-key",
		},
		server.Client(),
		server.URL+"/v1",
	)
	got, err := client.Propose(context.Background(), testModelRequest())
	if err != nil {
		t.Fatalf("Propose() error = %v", err)
	}
	if got.Summary != "Add a brass response" || got.Source != "pattern" {
		t.Errorf("proposal = %#v, want fenced JSON unwrapped", got)
	}
}

func openAICompatResponseForTest(proposal string) string {
	return `{"choices":[{"message":{"role":"assistant","content":` + jsonStringForTest(proposal) + `}}]}`
}

func jsonStringForTest(value string) string {
	encoded, err := json.Marshal(value)
	if err != nil {
		panic(err)
	}
	return string(encoded)
}
