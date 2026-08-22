package main

import (
	"context"
	"errors"
	"flag"
	"fmt"
	"io"
	"net/http"
	"os"
	"path/filepath"
	"sort"
	"strings"
	"time"

	"github.com/charmbracelet/bubbletea"
	"github.com/nyelonong/basso/internal/ai"
	"github.com/nyelonong/basso/internal/engine"
	"github.com/nyelonong/basso/internal/suggest"
	"github.com/pmezard/go-difflib/difflib"
)

const (
	maxCommandSourceBytes = 256 << 10
	maxCommandPromptBytes = 16 << 10
	evaluationTimeout     = 250 * time.Millisecond
)

const topLevelHelp = `Basso plays Fennel patterns and manages reviewable AI suggestions.

Usage:
  basso play <source.fnl>                        Play and hot-reload a pattern.
  basso <source.fnl>                             Alias for basso play.
  basso studio <source.fnl>                      Cockpit UI: live status plus AI candidate review (accepts suggestion flags).
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

type modelFactory func(ai.Config) (suggest.Model, error)

// programRunner is the slice of tea.Program studio needs: block on Run
// until the model quits, and Send messages from playback goroutines.
type programRunner interface {
	Run() (tea.Model, error)
	Send(msg tea.Msg)
}

// newTeaProgram is the production program factory; tests swap in a headless
// one.
func newTeaProgram(model tea.Model, opts ...tea.ProgramOption) programRunner {
	return tea.NewProgram(model, opts...)
}

type commandDependencies struct {
	stdout           io.Writer
	stderr           io.Writer
	getenv           func(string) string
	now              func() time.Time
	invocationDir    string
	storeRoot        string
	newModel         modelFactory
	newPreflighter   suggest.PreflighterFactory
	newProvider      providerConstructor
	newSink          func() engine.AudioSink
	newStudioProgram func(model tea.Model, opts ...tea.ProgramOption) programRunner
}

func defaultCommandDependencies(stdout, stderr io.Writer) (commandDependencies, error) {
	invocationDir, err := os.Getwd()
	if err != nil {
		return commandDependencies{}, fmt.Errorf("resolve invocation directory: %w", err)
	}
	return commandDependencies{
		stdout:         stdout,
		stderr:         stderr,
		getenv:         os.Getenv,
		now:            time.Now,
		invocationDir:  invocationDir,
		storeRoot:      filepath.Join(invocationDir, ".basso"),
		newModel:       newConcreteModel,
		newPreflighter: newEvaluatorPreflighter,
		newProvider:    newFennelProvider,
		newSink:        newBeepSink,
	}, nil
}

func runCommand(ctx context.Context, args []string, deps commandDependencies) error {
	deps = withCommandDefaults(deps)
	if len(args) > 0 {
		switch args[0] {
		case "help", "-h", "--help":
			if len(args) != 1 {
				return errors.New("top-level help does not accept arguments")
			}
			return writeTopLevelHelp(deps.stdout)
		case "suggest":
			return runSuggestCommand(ctx, args[1:], deps)
		case "studio":
			return runStudioCommand(ctx, args[1:], deps)
		case "apply":
			return runApplyCommand(ctx, args[1:], deps)
		}
	}
	return run(ctx, args, playbackObservers{
		onBar: func(bar, bpm, stepsPerBar int) {
			fmt.Fprintf(deps.stdout, "bar %d bpm %d\n", bar, bpm)
		},
		onDiagnostic: stderrDiagnosticReporter(deps.stderr),
	}, deps.newProvider, deps.newSink)
}

func writeTopLevelHelp(stdout io.Writer) error {
	if _, err := io.WriteString(stdout, topLevelHelp); err != nil {
		return fmt.Errorf("write top-level help: %w", err)
	}
	return nil
}

func withCommandDefaults(deps commandDependencies) commandDependencies {
	if deps.stdout == nil {
		deps.stdout = io.Discard
	}
	if deps.stderr == nil {
		deps.stderr = io.Discard
	}
	if deps.getenv == nil {
		deps.getenv = os.Getenv
	}
	if deps.now == nil {
		deps.now = time.Now
	}
	if deps.invocationDir == "" {
		deps.invocationDir = "."
	}
	if deps.storeRoot == "" {
		deps.storeRoot = filepath.Join(deps.invocationDir, ".basso")
	}
	if deps.newModel == nil {
		deps.newModel = newConcreteModel
	}
	if deps.newPreflighter == nil {
		deps.newPreflighter = newEvaluatorPreflighter
	}
	if deps.newProvider == nil {
		deps.newProvider = newFennelProvider
	}
	if deps.newSink == nil {
		deps.newSink = newBeepSink
	}
	if deps.newStudioProgram == nil {
		deps.newStudioProgram = newTeaProgram
	}
	return deps
}

func runSuggestCommand(ctx context.Context, args []string, deps commandDependencies) error {
	flags := flag.NewFlagSet("suggest", flag.ContinueOnError)
	flags.SetOutput(deps.stderr)
	var provider string
	var model string
	var timeout string
	var sounds string
	flags.StringVar(&provider, "provider", "", "AI provider: openai, ollama, or openai-compatible")
	flags.StringVar(&model, "model", "", "provider model name")
	flags.StringVar(&timeout, "timeout", "", "provider request timeout")
	flags.StringVar(&sounds, "sounds", "sound/808", "sound inventory directory")
	flags.Usage = func() {
		fmt.Fprintln(deps.stderr, "usage: basso suggest [flags] <source.fnl> <prompt>")
		flags.PrintDefaults()
	}
	if err := flags.Parse(args); err != nil {
		flags.Usage()
		return fmt.Errorf("suggest flags: %w", err)
	}
	positionals := flags.Args()
	if len(positionals) != 2 {
		flags.Usage()
		return fmt.Errorf("suggest requires exactly <source.fnl> <prompt>")
	}

	config, err := ai.ResolveConfig(ai.Overrides{
		Provider: provider,
		Model:    model,
		Timeout:  timeout,
	}, deps.getenv)
	if err != nil {
		return err
	}

	soundsPath, err := absoluteFrom(deps.invocationDir, sounds)
	if err != nil {
		return fmt.Errorf("resolve sounds path: %w", err)
	}
	inventory, err := engine.LoadSoundInventory(soundsPath)
	if err != nil {
		return err
	}

	sourcePath, source, err := loadCommandSource(deps.invocationDir, positionals[0])
	if err != nil {
		return err
	}
	prompt := positionals[1]
	if strings.TrimSpace(prompt) == "" {
		return errors.New("suggest prompt must not be empty")
	}
	if len(prompt) > maxCommandPromptBytes {
		return fmt.Errorf("suggest prompt exceeds %d bytes", maxCommandPromptBytes)
	}

	selectedModel, err := deps.newModel(config)
	if err != nil {
		return fmt.Errorf("construct %s model: %w", config.Provider, err)
	}
	if selectedModel == nil {
		return errors.New("model factory returned nil")
	}
	preflighter, err := deps.newPreflighter(soundsPath)
	if err != nil {
		return fmt.Errorf("construct preflighter: %w", err)
	}
	if preflighter == nil {
		return errors.New("preflighter factory returned nil")
	}

	candidate, err := suggest.NewService(selectedModel, preflighter).Suggest(ctx, suggest.SuggestInput{
		Provider:    config.Provider,
		Model:       config.Model,
		Prompt:      prompt,
		SourcePath:  sourcePath,
		SoundsPath:  soundsPath,
		Source:      source,
		Samples:     sortedSamples(inventory),
		Instruments: []string{"bass", "brass", "pluck"},
	})
	if err != nil {
		return err
	}

	storeRoot, err := absoluteFrom(deps.invocationDir, deps.storeRoot)
	if err != nil {
		return fmt.Errorf("resolve candidate store: %w", err)
	}
	saved, err := suggest.NewStore(storeRoot, deps.now).Save(candidate)
	if err != nil {
		return fmt.Errorf("save candidate: %w", err)
	}
	candidatePath := filepath.Join(storeRoot, "candidates", saved.Metadata.ID+".fnl")
	diff, err := difflib.GetUnifiedDiffString(difflib.UnifiedDiff{
		A:        difflib.SplitLines(string(source)),
		B:        difflib.SplitLines(string(saved.Source)),
		FromFile: sourcePath,
		ToFile:   candidatePath,
		Context:  3,
	})
	if err != nil {
		return fmt.Errorf("render candidate diff: %w", err)
	}
	if _, err := fmt.Fprintf(
		deps.stdout,
		"candidate ID: %s\nsummary: %s\nvalidation: %s\ncandidate path: %s\n%s",
		saved.Metadata.ID,
		saved.Metadata.Summary,
		saved.Metadata.Validation.Status,
		candidatePath,
		diff,
	); err != nil {
		return fmt.Errorf("write suggestion output: %w", err)
	}
	return nil
}

func runApplyCommand(ctx context.Context, args []string, deps commandDependencies) error {
	if len(args) != 1 || strings.TrimSpace(args[0]) == "" {
		fmt.Fprintln(deps.stderr, "usage: basso apply <candidate-id>")
		return errors.New("apply requires exactly one non-empty candidate ID")
	}
	storeRoot, err := absoluteFrom(deps.invocationDir, deps.storeRoot)
	if err != nil {
		return fmt.Errorf("resolve candidate store: %w", err)
	}
	result, err := suggest.NewApplier(
		suggest.NewStore(storeRoot, deps.now),
		deps.newPreflighter,
		deps.now,
	).Apply(ctx, args[0])
	if err != nil {
		return err
	}
	if _, err := fmt.Fprintf(
		deps.stdout,
		"source path: %s\nbackup path: %s\n",
		result.SourcePath,
		result.BackupPath,
	); err != nil {
		return fmt.Errorf("write apply output: %w", err)
	}
	return nil
}

func loadCommandSource(invocationDir, path string) (string, []byte, error) {
	absolutePath, err := absoluteFrom(invocationDir, path)
	if err != nil {
		return "", nil, fmt.Errorf("resolve source path: %w", err)
	}
	if filepath.Ext(absolutePath) != ".fnl" {
		return "", nil, errors.New("suggest source must have a .fnl extension")
	}
	info, err := os.Lstat(absolutePath)
	if err != nil {
		return "", nil, fmt.Errorf("inspect suggest source: %w", err)
	}
	if info.Mode()&os.ModeSymlink != 0 {
		return "", nil, errors.New("suggest source must not be a symbolic link")
	}
	if !info.Mode().IsRegular() {
		return "", nil, errors.New("suggest source must be a regular file")
	}
	if info.Size() > maxCommandSourceBytes {
		return "", nil, fmt.Errorf("suggest source exceeds %d bytes", maxCommandSourceBytes)
	}
	source, err := os.ReadFile(absolutePath)
	if err != nil {
		return "", nil, fmt.Errorf("read suggest source: %w", err)
	}
	if len(source) > maxCommandSourceBytes {
		return "", nil, fmt.Errorf("suggest source exceeds %d bytes", maxCommandSourceBytes)
	}
	return absolutePath, source, nil
}

func absoluteFrom(invocationDir, path string) (string, error) {
	if !filepath.IsAbs(path) {
		path = filepath.Join(invocationDir, path)
	}
	return filepath.Abs(path)
}

func sortedSamples(inventory engine.SoundInventory) []string {
	samples := make([]string, 0, len(inventory))
	for sample := range inventory {
		samples = append(samples, sample)
	}
	sort.Strings(samples)
	return samples
}

func newConcreteModel(config ai.Config) (suggest.Model, error) {
	switch config.Provider {
	case "openai":
		return ai.NewOpenAIClient(config, http.DefaultClient), nil
	case "ollama":
		return ai.NewOllamaClient(config, http.DefaultClient), nil
	case "openai-compatible":
		return ai.NewOpenAICompatClient(config, http.DefaultClient), nil
	default:
		return nil, fmt.Errorf("unsupported provider %q", config.Provider)
	}
}

type evaluatorPreflighter struct {
	evaluator *engine.Evaluator
}

func newEvaluatorPreflighter(soundsPath string) (suggest.Preflighter, error) {
	inventory, err := engine.LoadSoundInventory(soundsPath)
	if err != nil {
		return nil, err
	}
	return &evaluatorPreflighter{
		evaluator: engine.NewEvaluator(inventory, evaluationTimeout),
	}, nil
}

func (preflighter *evaluatorPreflighter) Preflight(
	ctx context.Context,
	source string,
	firstBar int,
	lastBar int,
) error {
	if preflighter == nil || preflighter.evaluator == nil {
		return errors.New("evaluator preflighter is nil")
	}
	for bar := firstBar; bar <= lastBar; bar++ {
		if _, err := preflighter.evaluator.Evaluate(ctx, source, bar); err != nil {
			return err
		}
	}
	return nil
}
