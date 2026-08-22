package ai

import (
	"testing"
	"time"
)

func TestResolveConfig_FlagsOverrideEnvironment(t *testing.T) {
	env := map[string]string{
		"BASSO_AI_PROVIDER": "ollama",
		"BASSO_AI_MODEL":    "environment-model",
		"BASSO_AI_TIMEOUT":  "17s",
		"OPENAI_API_KEY":    "test-secret",
	}

	config, err := ResolveConfig(Overrides{
		Provider: "openai",
		Model:    "flag-model",
		Timeout:  "3s",
	}, func(key string) string {
		return env[key]
	})
	if err != nil {
		t.Fatalf("ResolveConfig() error = %v", err)
	}
	if config.Provider != "openai" {
		t.Errorf("Provider = %q, want %q", config.Provider, "openai")
	}
	if config.Model != "flag-model" {
		t.Errorf("Model = %q, want %q", config.Model, "flag-model")
	}
	if config.Timeout != 3*time.Second {
		t.Errorf("Timeout = %v, want %v", config.Timeout, 3*time.Second)
	}
	if config.OpenAIAPIKey != "test-secret" {
		t.Errorf("OpenAIAPIKey = %q, want configured key", config.OpenAIAPIKey)
	}
}

func TestResolveConfig_RequiresProviderModelAndOpenAIKey(t *testing.T) {
	tests := []struct {
		name      string
		overrides Overrides
		env       map[string]string
	}{
		{
			name: "provider",
			env:  map[string]string{"BASSO_AI_MODEL": "model"},
		},
		{
			name:      "model",
			overrides: Overrides{Provider: "ollama"},
		},
		{
			name:      "model is whitespace only",
			overrides: Overrides{Provider: "ollama", Model: " \t "},
		},
		{
			name:      "flag model has surrounding whitespace",
			overrides: Overrides{Provider: "ollama", Model: " model"},
		},
		{
			name: "environment model has surrounding whitespace",
			env: map[string]string{
				"BASSO_AI_PROVIDER": "ollama",
				"BASSO_AI_MODEL":    "model ",
			},
		},
		{
			name:      "unsupported provider",
			overrides: Overrides{Provider: "other", Model: "model"},
		},
		{
			name:      "provider must be exact",
			overrides: Overrides{Provider: " openai ", Model: "model"},
			env:       map[string]string{"OPENAI_API_KEY": "test-secret"},
		},
		{
			name:      "OpenAI key",
			overrides: Overrides{Provider: "openai", Model: "model"},
		},
		{
			name:      "compatible API key",
			overrides: Overrides{Provider: "openai-compatible", Model: "model"},
			env:       map[string]string{"BASSO_AI_BASE_URL": "https://example.test/v1"},
		},
		{
			name:      "compatible base URL",
			overrides: Overrides{Provider: "openai-compatible", Model: "model"},
			env:       map[string]string{"BASSO_AI_API_KEY": "test-secret"},
		},
		{
			name:      "compatible whitespace key",
			overrides: Overrides{Provider: "openai-compatible", Model: "model"},
			env: map[string]string{
				"BASSO_AI_API_KEY":  " ",
				"BASSO_AI_BASE_URL": "https://example.test/v1",
			},
		},
		{
			name:      "compatible invalid URL scheme",
			overrides: Overrides{Provider: "openai-compatible", Model: "model"},
			env: map[string]string{
				"BASSO_AI_API_KEY":  "test-secret",
				"BASSO_AI_BASE_URL": "ftp://example.test/v1",
			},
		},
		{
			name:      "compatible URL with credentials",
			overrides: Overrides{Provider: "openai-compatible", Model: "model"},
			env: map[string]string{
				"BASSO_AI_API_KEY":  "test-secret",
				"BASSO_AI_BASE_URL": "https://user@example.test/v1",
			},
		},
		{
			name:      "compatible URL with query",
			overrides: Overrides{Provider: "openai-compatible", Model: "model"},
			env: map[string]string{
				"BASSO_AI_API_KEY":  "test-secret",
				"BASSO_AI_BASE_URL": "https://example.test/v1?x=1",
			},
		},
		{
			name:      "invalid timeout",
			overrides: Overrides{Provider: "ollama", Model: "model", Timeout: "soon"},
		},
		{
			name:      "non-positive timeout",
			overrides: Overrides{Provider: "ollama", Model: "model", Timeout: "0s"},
		},
		{
			name:      "timeout must parse strictly",
			overrides: Overrides{Provider: "ollama", Model: "model", Timeout: " 3s "},
		},
	}

	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			_, err := ResolveConfig(test.overrides, func(key string) string {
				return test.env[key]
			})
			if err == nil {
				t.Fatal("ResolveConfig() error = nil, want error")
			}
		})
	}
}

func TestResolveConfig_OpenAICompatible(t *testing.T) {
	env := map[string]string{
		"BASSO_AI_API_KEY":  "test-secret",
		"BASSO_AI_BASE_URL": "https://gateway.example/zen/go/v1/",
	}

	config, err := ResolveConfig(
		Overrides{Provider: "openai-compatible", Model: "free-model"},
		func(key string) string { return env[key] },
	)
	if err != nil {
		t.Fatalf("ResolveConfig() error = %v", err)
	}
	if config.OpenAICompatAPIKey != "test-secret" {
		t.Errorf("OpenAICompatAPIKey = %q, want configured key", config.OpenAICompatAPIKey)
	}
	if config.OpenAICompatBaseURL == nil {
		t.Fatal("OpenAICompatBaseURL = nil")
	}
	if got := config.OpenAICompatBaseURL.String(); got != "https://gateway.example/zen/go/v1" {
		t.Errorf("OpenAICompatBaseURL = %q, want %q", got, "https://gateway.example/zen/go/v1")
	}
}

func TestResolveConfig_DefaultsTimeoutAndOllamaURL(t *testing.T) {
	config, err := ResolveConfig(
		Overrides{Provider: "ollama", Model: "model"},
		func(string) string { return "" },
	)
	if err != nil {
		t.Fatalf("ResolveConfig() error = %v", err)
	}
	if config.Timeout != 180*time.Second {
		t.Errorf("Timeout = %v, want %v", config.Timeout, 180*time.Second)
	}
	if config.OllamaURL == nil {
		t.Fatal("OllamaURL = nil")
	}
	if got := config.OllamaURL.String(); got != "http://127.0.0.1:11434" {
		t.Errorf("OllamaURL = %q, want %q", got, "http://127.0.0.1:11434")
	}

	tests := []struct {
		name string
		raw  string
		want string
	}{
		{name: "normalizes path", raw: "https://ollama.example/some/path", want: "https://ollama.example"},
		{name: "normalizes root path", raw: "http://ollama.example/", want: "http://ollama.example"},
	}
	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			config, err := ResolveConfig(
				Overrides{Provider: "ollama", Model: "model"},
				func(key string) string {
					if key == "BASSO_OLLAMA_URL" {
						return test.raw
					}
					return ""
				},
			)
			if err != nil {
				t.Fatalf("ResolveConfig() error = %v", err)
			}
			if got := config.OllamaURL.String(); got != test.want {
				t.Errorf("OllamaURL = %q, want %q", got, test.want)
			}
		})
	}

	for _, raw := range []string{
		"ftp://ollama.example",
		"http://user:pass@ollama.example",
		"http://ollama.example?query=yes",
		"http://ollama.example?",
		"http://ollama.example#fragment",
		"http://ollama.example#",
		"http:///missing-host",
	} {
		t.Run("rejects "+raw, func(t *testing.T) {
			_, err := ResolveConfig(
				Overrides{Provider: "ollama", Model: "model"},
				func(key string) string {
					if key == "BASSO_OLLAMA_URL" {
						return raw
					}
					return ""
				},
			)
			if err == nil {
				t.Fatal("ResolveConfig() error = nil, want error")
			}
		})
	}
}
