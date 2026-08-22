package main

import (
	"os"
	"path/filepath"
	"testing"
)

func TestLoadEnvFile(t *testing.T) {
	dir := t.TempDir()
	path := filepath.Join(dir, ".env")
	content := "" +
		"# provider config\n" +
		"BASSO_AI_PROVIDER=openai-compatible\n" +
		"\n" +
		"BASSO_AI_MODEL=\"quoted model\"\n" +
		"INVALID line without equals\n" +
		"BASSO_AI_BASE_URL=https://gateway.test/v1 # trailing comment\n"
	if err := os.WriteFile(path, []byte(content), 0o600); err != nil {
		t.Fatal(err)
	}

	env := loadEnvFile(path)
	want := map[string]string{
		"BASSO_AI_PROVIDER": "openai-compatible",
		"BASSO_AI_MODEL":    "quoted model",
		"BASSO_AI_BASE_URL": "https://gateway.test/v1",
	}
	for key, wantValue := range want {
		if got := env[key]; got != wantValue {
			t.Errorf("env[%q] = %q, want %q", key, got, wantValue)
		}
	}
	if len(env) != len(want) {
		t.Errorf("env has %d entries, want %d", len(env), len(want))
	}
}

func TestLoadEnvFile_MissingFileIsEmpty(t *testing.T) {
	env := loadEnvFile(filepath.Join(t.TempDir(), "absent.env"))
	if len(env) != 0 {
		t.Errorf("env = %v, want empty for missing file", env)
	}
}

func TestGetenvWithFile_RealEnvironmentWins(t *testing.T) {
	fileValues := map[string]string{
		"BASSO_AI_MODEL": "file-model",
		"OTHER_KEY":      "from-file",
	}
	getenv := getenvWithFile(func(key string) string {
		if key == "BASSO_AI_MODEL" {
			return "real-model"
		}
		return ""
	}, fileValues)

	if got := getenv("BASSO_AI_MODEL"); got != "real-model" {
		t.Errorf("getenv = %q, want real environment to win", got)
	}
	if got := getenv("OTHER_KEY"); got != "from-file" {
		t.Errorf("getenv = %q, want .env fallback", got)
	}
	if got := getenv("UNSET_EVERYWHERE"); got != "" {
		t.Errorf("getenv = %q, want empty", got)
	}
}
