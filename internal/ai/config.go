package ai

import (
	"errors"
	"fmt"
	"net/url"
	"strings"
	"time"
)

const (
	defaultTimeout   = 60 * time.Second
	defaultOllamaURL = "http://127.0.0.1:11434"
)

type Overrides struct {
	Provider string
	Model    string
	Timeout  string
}

type Config struct {
	Provider     string
	Model        string
	Timeout      time.Duration
	OpenAIAPIKey string
	OllamaURL    *url.URL
}

func ResolveConfig(overrides Overrides, getenv func(string) string) (Config, error) {
	if getenv == nil {
		return Config{}, errors.New("ai: environment lookup is nil")
	}

	provider := resolveValue(overrides.Provider, getenv("BASSO_AI_PROVIDER"))
	model := resolveValue(overrides.Model, getenv("BASSO_AI_MODEL"))
	timeoutValue := resolveValue(overrides.Timeout, getenv("BASSO_AI_TIMEOUT"))

	if strings.TrimSpace(provider) == "" {
		return Config{}, errors.New("ai: provider is required")
	}
	trimmedModel := strings.TrimSpace(model)
	if trimmedModel == "" {
		return Config{}, errors.New("ai: model is required")
	}
	if model != trimmedModel {
		return Config{}, errors.New("ai: model must not contain surrounding whitespace")
	}
	if provider != "openai" && provider != "ollama" {
		return Config{}, fmt.Errorf("ai: unsupported provider %q", provider)
	}

	timeout := defaultTimeout
	if timeoutValue != "" {
		parsed, err := time.ParseDuration(timeoutValue)
		if err != nil {
			return Config{}, fmt.Errorf("ai: parse timeout: %w", err)
		}
		if parsed <= 0 {
			return Config{}, errors.New("ai: timeout must be positive")
		}
		timeout = parsed
	}

	config := Config{
		Provider: provider,
		Model:    model,
		Timeout:  timeout,
	}
	if provider == "openai" {
		config.OpenAIAPIKey = getenv("OPENAI_API_KEY")
		if strings.TrimSpace(config.OpenAIAPIKey) == "" {
			return Config{}, errors.New("ai: OPENAI_API_KEY is required for openai")
		}
		return config, nil
	}

	ollamaURL := getenv("BASSO_OLLAMA_URL")
	if ollamaURL == "" {
		ollamaURL = defaultOllamaURL
	}
	origin, err := normalizeHTTPOrigin(ollamaURL)
	if err != nil {
		return Config{}, fmt.Errorf("ai: invalid BASSO_OLLAMA_URL: %w", err)
	}
	config.OllamaURL = origin

	return config, nil
}

func resolveValue(override string, environment string) string {
	if override != "" {
		return override
	}
	return environment
}

func normalizeHTTPOrigin(raw string) (*url.URL, error) {
	parsed, err := url.Parse(raw)
	if err != nil {
		return nil, fmt.Errorf("parse URL: %w", err)
	}
	if parsed.Scheme != "http" && parsed.Scheme != "https" {
		return nil, errors.New("scheme must be http or https")
	}
	if parsed.Host == "" {
		return nil, errors.New("host is required")
	}
	if parsed.User != nil {
		return nil, errors.New("credentials are not allowed")
	}
	if parsed.RawQuery != "" || parsed.ForceQuery {
		return nil, errors.New("query is not allowed")
	}
	if parsed.Fragment != "" || strings.Contains(raw, "#") {
		return nil, errors.New("fragment is not allowed")
	}

	return &url.URL{
		Scheme: parsed.Scheme,
		Host:   parsed.Host,
	}, nil
}
