package main

import (
	"bytes"
	"context"
	"crypto/sha256"
	"errors"
	"fmt"
	"io"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"github.com/nyelonong/basso/internal/ai"
	"github.com/nyelonong/basso/internal/engine"
	"github.com/nyelonong/basso/internal/suggest"
)

var commandTestNow = time.Date(2026, time.July, 25, 12, 0, 0, 0, time.UTC)

type fakeCommandModel struct {
	proposal suggest.Proposal
	err      error
	calls    int
}

func (model *fakeCommandModel) Propose(context.Context, suggest.ModelRequest) (suggest.Proposal, error) {
	model.calls++
	return model.proposal, model.err
}

type fakeCommandPreflighter struct {
	err   error
	calls int
}

func (preflighter *fakeCommandPreflighter) Preflight(context.Context, string, int, int) error {
	preflighter.calls++
	return preflighter.err
}

func TestHelpCommand_ListsAllCommandsAndConfiguration(t *testing.T) {
	const want = `Basso plays Fennel patterns and manages reviewable AI suggestions.

Usage:
  basso play <source.fnl>                        Play and hot-reload a pattern.
  basso <source.fnl>                             Alias for basso play.
  basso suggest [flags] <source.fnl> <prompt>    Create and review a candidate.
  basso apply <candidate-id>                     Apply a validated candidate.
  basso help                                     Show this help.
  basso -h                                       Show this help.
  basso --help                                   Show this help.

Suggestion flags:
  --provider <openai|ollama|openai-compatible>  AI provider (required).
  --model <name>              Provider model name (required).
  --timeout <duration>        Provider request timeout (default 60s).
  --sounds <path>             Sound inventory directory (default sound/808).

Provider environment:
  BASSO_AI_PROVIDER  Default AI provider.
  BASSO_AI_MODEL     Default provider model.
  BASSO_AI_TIMEOUT   Default provider request timeout.
  OPENAI_API_KEY     API key required by the OpenAI provider.
  BASSO_OLLAMA_URL   Ollama base URL (default http://127.0.0.1:11434).
  BASSO_AI_BASE_URL  Base URL required by the openai-compatible provider.
  BASSO_AI_API_KEY   API key required by the openai-compatible provider.
`
	var stdout bytes.Buffer
	deps := testCommandDependencies(t.TempDir(), &stdout, io.Discard)

	if err := runCommand(context.Background(), []string{"help"}, deps); err != nil {
		t.Fatalf("runCommand(help) error = %v", err)
	}
	if got := stdout.String(); got != want {
		t.Errorf("help output mismatch\n got: %q\nwant: %q", got, want)
	}
}

func TestHelpCommand_AliasesMatch(t *testing.T) {
	aliases := [][]string{{"help"}, {"-h"}, {"--help"}}
	outputs := make([]string, 0, len(aliases))
	for _, args := range aliases {
		var stdout bytes.Buffer
		deps := testCommandDependencies(t.TempDir(), &stdout, io.Discard)

		if err := runCommand(context.Background(), args, deps); err != nil {
			t.Fatalf("runCommand(%q) error = %v", args, err)
		}
		outputs = append(outputs, stdout.String())
	}

	for index := 1; index < len(outputs); index++ {
		if outputs[index] != outputs[0] {
			t.Errorf("help output for %q differs from help output", aliases[index])
		}
	}
}

func TestHelpCommand_HasNoSideEffects(t *testing.T) {
	dir := t.TempDir()
	var stdout bytes.Buffer
	var stderr bytes.Buffer
	var operationalCalls []string
	unexpected := func(name string) error {
		operationalCalls = append(operationalCalls, name)
		return fmt.Errorf("unexpected %s call", name)
	}
	deps := commandDependencies{
		stdout:        &stdout,
		stderr:        &stderr,
		invocationDir: dir,
		storeRoot:     filepath.Join(dir, ".basso"),
		getenv: func(string) string {
			_ = unexpected("environment")
			return ""
		},
		now: func() time.Time {
			_ = unexpected("clock")
			return commandTestNow
		},
		newModel: func(ai.Config) (suggest.Model, error) {
			return nil, unexpected("model")
		},
		newPreflighter: func(string) (suggest.Preflighter, error) {
			return nil, unexpected("preflight")
		},
		newProvider: func(string) (closablePatternProvider, error) {
			return nil, unexpected("playback provider")
		},
		newSink: func() engine.AudioSink {
			_ = unexpected("audio")
			return nil
		},
	}

	if err := runCommand(context.Background(), []string{"help"}, deps); err != nil {
		t.Fatalf("runCommand(help) error = %v", err)
	}
	if len(operationalCalls) != 0 {
		t.Errorf("operational calls = %v, want none", operationalCalls)
	}
	if stderr.Len() != 0 {
		t.Errorf("stderr = %q, want empty", stderr.String())
	}
	if _, err := os.Stat(filepath.Join(dir, ".basso")); !os.IsNotExist(err) {
		t.Errorf(".basso stat error = %v, want not exist", err)
	}
}

func TestHelpCommand_RejectsArguments(t *testing.T) {
	tests := []struct {
		name string
		args []string
	}{
		{name: "help", args: []string{"help", "extra"}},
		{name: "short alias", args: []string{"-h", "extra"}},
		{name: "long alias", args: []string{"--help", "extra"}},
	}

	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			var stdout bytes.Buffer
			deps := testCommandDependencies(t.TempDir(), &stdout, io.Discard)

			err := runCommand(context.Background(), test.args, deps)

			if err == nil {
				t.Fatal("runCommand() error = nil, want trailing-argument error")
			}
			if stdout.Len() != 0 {
				t.Errorf("stdout = %q, want no successful help output", stdout.String())
			}
		})
	}
}

func TestSuggestCommand_ValidatesConfigurationBeforeSourceRead(t *testing.T) {
	dir := t.TempDir()
	var stderr bytes.Buffer
	modelFactoryCalls := 0
	deps := testCommandDependencies(dir, io.Discard, &stderr)
	deps.newModel = func(ai.Config) (suggest.Model, error) {
		modelFactoryCalls++
		return nil, errors.New("unexpected model construction")
	}

	err := runCommand(
		context.Background(),
		[]string{
			"suggest",
			"--provider", "unsupported",
			"--model", "test-model",
			filepath.Join(dir, "missing.fnl"),
			"change it",
		},
		deps,
	)

	if err == nil {
		t.Fatal("runCommand() error = nil, want configuration error")
	}
	if !strings.Contains(err.Error(), "unsupported provider") {
		t.Errorf("error = %q, want unsupported provider", err)
	}
	if modelFactoryCalls != 0 {
		t.Errorf("model factory calls = %d, want 0", modelFactoryCalls)
	}
	if _, statErr := os.Stat(filepath.Join(dir, ".basso")); !os.IsNotExist(statErr) {
		t.Errorf(".basso stat error = %v, want not exist", statErr)
	}
}

func TestSuggestCommand_NeverStartsAudio(t *testing.T) {
	dir, sourcePath, _ := newCommandFixture(t)
	model := &fakeCommandModel{
		proposal: suggest.Proposal{Summary: "made it quieter", Source: "(candidate)\n"},
	}
	sinkConstructions := 0
	deps := testCommandDependencies(dir, io.Discard, io.Discard)
	deps.newModel = func(ai.Config) (suggest.Model, error) { return model, nil }
	deps.newSink = func() engine.AudioSink {
		sinkConstructions++
		return newFakeSink()
	}

	err := runCommand(context.Background(), suggestArgs(sourcePath), deps)

	if err != nil {
		t.Fatalf("runCommand() error = %v", err)
	}
	if sinkConstructions != 0 {
		t.Errorf("audio sink constructions = %d, want 0", sinkConstructions)
	}
}

func TestSuggestCommand_SavesCandidateAndPrintsUnifiedDiff(t *testing.T) {
	dir, sourcePath, original := newCommandFixture(t)
	candidateSource := "(fn pattern [bar]\n  {:bpm 90 :steps 16 :hits []})\n\npattern\n"
	model := &fakeCommandModel{
		proposal: suggest.Proposal{
			Summary: "slowed the groove",
			Source:  candidateSource,
		},
	}
	var stdout bytes.Buffer
	deps := testCommandDependencies(dir, &stdout, io.Discard)
	deps.newModel = func(ai.Config) (suggest.Model, error) { return model, nil }

	if err := runCommand(context.Background(), suggestArgs(sourcePath), deps); err != nil {
		t.Fatalf("runCommand() error = %v", err)
	}

	if got := readCommandFile(t, sourcePath); !bytes.Equal(got, original) {
		t.Errorf("active source changed: got %q, want %q", got, original)
	}
	candidateFiles, err := filepath.Glob(filepath.Join(dir, ".basso", "candidates", "*.fnl"))
	if err != nil {
		t.Fatalf("glob candidates: %v", err)
	}
	if len(candidateFiles) != 1 {
		t.Fatalf("candidate source count = %d, want 1", len(candidateFiles))
	}
	if got := readCommandFile(t, candidateFiles[0]); string(got) != candidateSource {
		t.Errorf("candidate source = %q, want %q", got, candidateSource)
	}
	output := stdout.String()
	for _, want := range []string{
		"candidate ID:",
		"summary: slowed the groove",
		"validation: passed",
		"candidate path: " + candidateFiles[0],
		"--- " + sourcePath,
		"+++ " + candidateFiles[0],
		"-  {:bpm 120 :steps 16 :hits []}",
		"+  {:bpm 90 :steps 16 :hits []}",
	} {
		if !strings.Contains(output, want) {
			t.Errorf("stdout = %q, want substring %q", output, want)
		}
	}
}

func TestSuggestCommand_ProviderFailureCreatesNoCandidate(t *testing.T) {
	dir, sourcePath, original := newCommandFixture(t)
	providerErr := errors.New("provider unavailable")
	model := &fakeCommandModel{err: providerErr}
	deps := testCommandDependencies(dir, io.Discard, io.Discard)
	deps.newModel = func(ai.Config) (suggest.Model, error) { return model, nil }

	err := runCommand(context.Background(), suggestArgs(sourcePath), deps)

	if !errors.Is(err, providerErr) {
		t.Fatalf("runCommand() error = %v, want %v", err, providerErr)
	}
	if got := readCommandFile(t, sourcePath); !bytes.Equal(got, original) {
		t.Errorf("active source changed: got %q, want %q", got, original)
	}
	candidates, globErr := filepath.Glob(filepath.Join(dir, ".basso", "candidates", "*"))
	if globErr != nil {
		t.Fatalf("glob candidates: %v", globErr)
	}
	if len(candidates) != 0 {
		t.Errorf("candidate artifacts = %v, want none", candidates)
	}
}

func TestApplyCommand_PrintsSourceAndBackup(t *testing.T) {
	dir, sourcePath, original := newCommandFixture(t)
	candidate := saveCommandCandidate(t, dir, sourcePath, original)
	var stdout bytes.Buffer
	deps := testCommandDependencies(dir, &stdout, io.Discard)

	if err := runCommand(context.Background(), []string{"apply", candidate.Metadata.ID}, deps); err != nil {
		t.Fatalf("runCommand() error = %v", err)
	}

	if got := readCommandFile(t, sourcePath); !bytes.Equal(got, candidate.Source) {
		t.Errorf("applied source = %q, want %q", got, candidate.Source)
	}
	output := stdout.String()
	if !strings.Contains(output, "source path: "+sourcePath) {
		t.Errorf("stdout = %q, want source path", output)
	}
	backupPrefix := "backup path: " + filepath.Join(dir, ".basso", "backups") + string(os.PathSeparator)
	if !strings.Contains(output, backupPrefix) {
		t.Errorf("stdout = %q, want backup prefix %q", output, backupPrefix)
	}
}

func TestApplyCommand_StaleCandidateLeavesSource(t *testing.T) {
	dir, sourcePath, original := newCommandFixture(t)
	candidate := saveCommandCandidate(t, dir, sourcePath, original)
	manualEdit := []byte("(manual edit)\n")
	if err := os.WriteFile(sourcePath, manualEdit, 0o600); err != nil {
		t.Fatalf("write manual edit: %v", err)
	}
	preflightFactoryCalls := 0
	deps := testCommandDependencies(dir, io.Discard, io.Discard)
	deps.newPreflighter = func(string) (suggest.Preflighter, error) {
		preflightFactoryCalls++
		return &fakeCommandPreflighter{}, nil
	}

	err := runCommand(context.Background(), []string{"apply", candidate.Metadata.ID}, deps)

	if err == nil {
		t.Fatal("runCommand() error = nil, want stale candidate error")
	}
	if got := readCommandFile(t, sourcePath); !bytes.Equal(got, manualEdit) {
		t.Errorf("source = %q, want manual edit %q", got, manualEdit)
	}
	if preflightFactoryCalls != 0 {
		t.Errorf("preflight factory calls = %d, want 0", preflightFactoryCalls)
	}
	if _, statErr := os.Stat(filepath.Join(dir, ".basso", "backups")); !os.IsNotExist(statErr) {
		t.Errorf("backups stat error = %v, want not exist", statErr)
	}
}

func TestCommandUsageAndArgumentErrors(t *testing.T) {
	tests := []struct {
		name string
		args []string
	}{
		{name: "suggest missing arguments", args: []string{"suggest"}},
		{name: "suggest extra argument", args: []string{"suggest", "one.fnl", "prompt", "extra"}},
		{name: "suggest invalid flag", args: []string{"suggest", "--wat"}},
		{name: "apply missing ID", args: []string{"apply"}},
		{name: "apply empty ID", args: []string{"apply", ""}},
		{name: "apply extra argument", args: []string{"apply", "one", "two"}},
	}

	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			var stderr bytes.Buffer
			deps := testCommandDependencies(t.TempDir(), io.Discard, &stderr)

			err := runCommand(context.Background(), test.args, deps)

			if err == nil {
				t.Fatal("runCommand() error = nil, want argument error")
			}
			if stderr.Len() == 0 {
				t.Fatal("stderr is empty, want usage or argument diagnostic")
			}
		})
	}
}

func testCommandDependencies(dir string, stdout, stderr io.Writer) commandDependencies {
	return commandDependencies{
		stdout:        stdout,
		stderr:        stderr,
		getenv:        func(string) string { return "" },
		now:           func() time.Time { return commandTestNow },
		invocationDir: dir,
		storeRoot:     filepath.Join(dir, ".basso"),
		newModel: func(ai.Config) (suggest.Model, error) {
			return nil, errors.New("unexpected model construction")
		},
		newPreflighter: func(string) (suggest.Preflighter, error) {
			return &fakeCommandPreflighter{}, nil
		},
		newProvider: func(string) (closablePatternProvider, error) {
			return nil, errors.New("unexpected playback")
		},
		newSink: newFakeSink,
	}
}

func suggestArgs(sourcePath string) []string {
	return []string{
		"suggest",
		"--provider", "ollama",
		"--model", "test-model",
		"--sounds", "sound/808",
		sourcePath,
		"change the groove",
	}
}

func newCommandFixture(t *testing.T) (string, string, []byte) {
	t.Helper()
	dir := t.TempDir()
	soundsPath := filepath.Join(dir, "sound", "808")
	if err := os.MkdirAll(soundsPath, 0o700); err != nil {
		t.Fatalf("create sounds: %v", err)
	}
	if err := os.WriteFile(filepath.Join(soundsPath, "kick.wav"), []byte("sample"), 0o600); err != nil {
		t.Fatalf("write sound: %v", err)
	}
	original := []byte("(fn pattern [bar]\n  {:bpm 120 :steps 16 :hits []})\n\npattern\n")
	sourcePath := filepath.Join(dir, "pattern.fnl")
	if err := os.WriteFile(sourcePath, original, 0o600); err != nil {
		t.Fatalf("write source: %v", err)
	}
	return dir, sourcePath, original
}

func saveCommandCandidate(t *testing.T, dir, sourcePath string, original []byte) suggest.Candidate {
	t.Helper()
	sum := sha256.Sum256(original)
	candidate, err := suggest.NewStore(
		filepath.Join(dir, ".basso"),
		func() time.Time { return commandTestNow },
	).Save(suggest.Candidate{
		Metadata: suggest.Metadata{
			SourcePath: sourcePath,
			SoundsPath: filepath.Join(dir, "sound", "808"),
			BaseSHA256: fmt.Sprintf("%x", sum),
			Provider:   "ollama",
			Model:      "test-model",
			Prompt:     "change the groove",
			Summary:    "changed the groove",
			Attempts:   1,
			Validation: suggest.ValidationRecord{
				FirstBar:        0,
				LastBar:         15,
				TimeoutMSPerBar: 250,
				Status:          "passed",
			},
		},
		Source: []byte("(candidate)\n"),
	})
	if err != nil {
		t.Fatalf("save candidate: %v", err)
	}
	return candidate
}

func readCommandFile(t *testing.T, path string) []byte {
	t.Helper()
	data, err := os.ReadFile(path)
	if err != nil {
		t.Fatalf("read %s: %v", path, err)
	}
	return data
}
