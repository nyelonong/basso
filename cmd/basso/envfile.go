// .env fallback loading: a KEY=VALUE file next to where basso runs provides
// defaults for provider configuration without touching shell profiles. Real
// environment variables always win.
package main

import (
	"os"
	"strings"
)

// loadEnvFile parses a simple KEY=VALUE file: `#` comments, blank lines,
// optional surrounding quotes on values, and everything after an unquoted
// value's whitespace treated as a comment. A missing or unreadable file
// yields an empty map.
func loadEnvFile(path string) map[string]string {
	data, err := os.ReadFile(path)
	if err != nil {
		return map[string]string{}
	}

	env := map[string]string{}
	for _, line := range strings.Split(string(data), "\n") {
		line = strings.TrimSpace(line)
		if line == "" || strings.HasPrefix(line, "#") {
			continue
		}
		key, value, found := strings.Cut(line, "=")
		if !found {
			continue
		}
		key = strings.TrimSpace(key)
		value = strings.TrimSpace(value)
		if len(value) >= 2 && (value[0] == '"' && value[len(value)-1] == '"' || value[0] == '\'' && value[len(value)-1] == '\'') {
			value = value[1 : len(value)-1]
		} else if idx := strings.IndexAny(value, " \t#"); idx >= 0 {
			value = strings.TrimSpace(value[:idx])
		}
		if key != "" {
			env[key] = value
		}
	}
	return env
}

// getenvWithFile wraps an environment lookup so real variables win and the
// loaded .env map supplies fallbacks.
func getenvWithFile(base func(string) string, fileValues map[string]string) func(string) string {
	return func(key string) string {
		if value := base(key); value != "" {
			return value
		}
		return fileValues[key]
	}
}
