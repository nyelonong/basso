// basso studio: a full-screen cockpit over playback — live bar/BPM/steps
// status plus the AI candidate review loop. Playback reuses the exact play
// machinery; the UI only observes it.
package main

import (
	"context"
	"errors"
	"flag"
	"fmt"
	"os"
	"path/filepath"
	"strings"

	"github.com/charmbracelet/bubbles/textinput"
	"github.com/charmbracelet/bubbletea"
	"github.com/nyelonong/basso/internal/ai"
	"github.com/nyelonong/basso/internal/engine"
	"github.com/nyelonong/basso/internal/suggest"
	"github.com/pmezard/go-difflib/difflib"
)

type barMsg struct {
	bar         int
	bpm         int
	stepsPerBar int
}

type diagnosticMsg struct {
	diagnostic engine.Diagnostic
}

type playbackDoneMsg struct {
	err error
}

type suggestPromptMsg struct{}

type suggestionReadyMsg struct {
	generation int
	candidate  suggest.Candidate
}

type suggestionFailedMsg struct {
	generation int
	err        error
}

type candidateAppliedMsg struct {
	id         string
	backupPath string
}

type applyFailedMsg struct {
	id  string
	err error
}

// studioServices carries the lazily-resolved AI dependencies behind the
// cockpit's suggest action.
type studioServices struct {
	overrides      ai.Overrides
	getenv         func(string) string
	invocationDir  string
	soundsPath     string
	newModel       modelFactory
	newPreflighter suggest.PreflighterFactory
}

func (deps commandDependencies) studioServices(overrides ai.Overrides, soundsPath string) studioServices {
	return studioServices{
		overrides:      overrides,
		getenv:         deps.getenv,
		invocationDir:  deps.invocationDir,
		soundsPath:     soundsPath,
		newModel:       deps.newModel,
		newPreflighter: deps.newPreflighter,
	}
}

type studioMode int

const (
	studioIdle studioMode = iota
	studioPrompting
	studioRunning
)

type studioModel struct {
	sourceName          string
	bar                 int
	bpm                 int
	stepsPerBar         int
	played              bool
	playbackErr         string
	diagnosticsObserved int
	events              []string

	mode           studioMode
	prompt         textinput.Model
	generation     int
	pendingSuggest int
	candidate      *suggest.Candidate
	diff           string
	lastError      string
	cancelSuggest  context.CancelFunc
	sourcePath     string
	services       studioServices
	store          *suggest.Store
}

// maxStudioEvents caps the on-screen event log; older reload diagnostics
// scroll off.
const maxStudioEvents = 8

func newStudioModel(sourceName string) studioModel {
	return studioModel{sourceName: sourceName}
}

func (m studioModel) Init() tea.Cmd { return nil }

func (m studioModel) Update(msg tea.Msg) (tea.Model, tea.Cmd) {
	switch msg := msg.(type) {
	case tea.KeyMsg:
		switch msg.String() {
		case "q", "ctrl+c":
			return m, tea.Quit
		case "s":
			if m.mode != studioIdle {
				break
			}
			input := textinput.New()
			input.Placeholder = "describe the change"
			input.Focus()
			m.prompt = input
			m.mode = studioPrompting
			return m, textinput.Blink
		case "esc":
			switch m.mode {
			case studioPrompting:
				m.mode = studioIdle
				return m, nil
			case studioRunning:
				m.cancelSuggest()
				m.mode = studioIdle
				m.pendingSuggest = 0
				m.events = append(m.events, "suggest cancelled")
				return m, nil
			}
		case "a":
			if m.candidate == nil || m.mode != studioIdle || m.store == nil {
				break
			}
			id := m.candidate.Metadata.ID
			store, preflighter := m.store, m.services.newPreflighter
			return m, func() tea.Msg {
				result, err := suggest.NewApplier(store, preflighter, nil).Apply(context.Background(), id)
				if err != nil {
					return applyFailedMsg{id: id, err: err}
				}
				return candidateAppliedMsg{id: id, backupPath: result.BackupPath}
			}
		case "r":
			if m.candidate == nil || m.mode != studioIdle {
				break
			}
			m.candidate = nil
			m.diff = ""
			m.events = append(m.events, "candidate rejected")
		case "enter":
			if m.mode == studioPrompting {
				request := strings.TrimSpace(m.prompt.Value())
				if request == "" {
					return m, nil
				}
				m.generation++
				generation := m.generation
				m.pendingSuggest = generation
				ctx, cancel := context.WithCancel(context.Background())
				m.cancelSuggest = cancel
				m.mode = studioRunning
				m.lastError = ""
				base := m.suggestWithCtx(ctx, m.services, m.sourcePath, request)
				return m, func() tea.Msg {
					switch tagged := base().(type) {
					case suggestionReadyMsg:
						tagged.generation = generation
						return tagged
					case suggestionFailedMsg:
						tagged.generation = generation
						return tagged
					default:
						return tagged
					}
				}
			}
		}
		if m.mode == studioPrompting {
			var cmd tea.Cmd
			m.prompt, cmd = m.prompt.Update(msg)
			return m, cmd
		}
	case suggestPromptMsg:
		if m.mode != studioIdle {
			return m, nil
		}
		input := textinput.New()
		input.Placeholder = "describe the change"
		input.Focus()
		m.prompt = input
		m.mode = studioPrompting
		return m, textinput.Blink
	case barMsg:
		m.played = true
		m.bar = msg.bar
		m.bpm = msg.bpm
		m.stepsPerBar = msg.stepsPerBar
	case diagnosticMsg:
		m.diagnosticsObserved++
		m.events = append(m.events, formatStudioEvent(msg.diagnostic))
		if len(m.events) > maxStudioEvents {
			m.events = m.events[len(m.events)-maxStudioEvents:]
		}
	case playbackDoneMsg:
		if msg.err != nil {
			m.playbackErr = msg.err.Error()
		}
		return m, tea.Quit
	case suggestionReadyMsg:
		if msg.generation == 0 || msg.generation != m.pendingSuggest {
			return m, nil
		}
		m.pendingSuggest = 0
		m.mode = studioIdle
		m.lastError = ""
		candidate := msg.candidate
		if m.store != nil {
			saved, err := m.store.Save(candidate)
			if err != nil {
				m.lastError = "save candidate: " + err.Error()
				return m, nil
			}
			candidate = saved
		}
		m.candidate = &candidate
		m.diff = m.renderDiff()
	case candidateAppliedMsg:
		m.candidate = nil
		m.diff = ""
		m.lastError = ""
		m.events = append(m.events, fmt.Sprintf("applied %s backup %s", shortID(msg.id), msg.backupPath))
	case applyFailedMsg:
		m.lastError = "apply " + shortID(msg.id) + ": " + msg.err.Error()
	case suggestionFailedMsg:
		if msg.generation != m.generation && m.mode != studioRunning {
			return m, nil
		}
		m.mode = studioIdle
		m.lastError = msg.err.Error()
	}
	return m, nil
}

func (m studioModel) View() string {
	var out strings.Builder
	out.WriteString("basso studio — " + m.sourceName + "\n")
	if !m.played {
		out.WriteString("waiting for first bar…\n")
	} else {
		out.WriteString(fmt.Sprintf("bar %d bpm %d steps %d\n", m.bar, m.bpm, m.stepsPerBar))
	}
	if m.playbackErr != "" {
		out.WriteString("playback stopped: " + m.playbackErr + "\n")
	}
	if len(m.events) > 0 {
		out.WriteString("\nevents:\n")
		for _, event := range m.events {
			out.WriteString(event + "\n")
		}
	}
	switch m.mode {
	case studioPrompting:
		out.WriteString("\nsuggest: " + m.prompt.View() + "\nenter send · esc back\n")
	case studioRunning:
		out.WriteString("\nsuggest: consulting provider… (esc cancel)\n")
	}
	if m.lastError != "" {
		out.WriteString("suggest failed: " + m.lastError + "\n")
		out.WriteString("hint: set --provider/--model, BASSO_AI_PROVIDER, BASSO_AI_MODEL, BASSO_AI_BASE_URL, BASSO_AI_API_KEY, or a .env file here\n")
	}
	if m.candidate != nil {
		out.WriteString(fmt.Sprintf("candidate %s: %s [validation %s]\n",
			shortID(m.candidate.Metadata.ID), m.candidate.Metadata.Summary, m.candidate.Metadata.Validation.Status))
		if m.diff != "" {
			out.WriteString("\ndiff:\n" + m.diff)
		}
	}
	out.WriteString("\nq quit · s suggest · a apply · r reject\n")
	return out.String()
}

// renderDiff diffs the on-disk source against the pending candidate.
func (m studioModel) renderDiff() string {
	current, err := os.ReadFile(m.sourcePath)
	if err != nil {
		return "(source unreadable: " + err.Error() + ")"
	}
	diff, err := difflib.GetUnifiedDiffString(difflib.UnifiedDiff{
		A:        difflib.SplitLines(string(current)),
		B:        difflib.SplitLines(string(m.candidate.Source)),
		FromFile: m.sourcePath,
		ToFile:   "candidate",
		Context:  3,
	})
	if err != nil {
		return "(diff unavailable)"
	}
	return diff
}

func shortID(id string) string {
	if len(id) > 12 {
		return id[:12]
	}
	return id
}

// newSuggestCmd builds the async request without owning its cancellation;
// Enter-driven submissions wrap it with a per-request context instead.
func (m studioModel) newSuggestCmd(services studioServices, sourcePath, request string) tea.Cmd {
	return func() tea.Msg {
		return runSuggestRequest(context.Background(), services, sourcePath, request)
	}
}

func (m studioModel) suggestWithCtx(
	ctx context.Context,
	services studioServices,
	sourcePath, request string,
) tea.Cmd {
	return func() tea.Msg {
		return runSuggestRequest(ctx, services, sourcePath, request)
	}
}

func runSuggestRequest(
	ctx context.Context,
	services studioServices,
	sourcePath, request string,
) tea.Msg {
	fail := func(err error) tea.Msg { return suggestionFailedMsg{err: err} }

	soundsPath := services.soundsPath
	if !filepath.IsAbs(soundsPath) {
		resolved, err := absoluteFrom(services.invocationDir, soundsPath)
		if err != nil {
			return fail(fmt.Errorf("resolve sounds path: %w", err))
		}
		soundsPath = resolved
	}

	config, err := ai.ResolveConfig(services.overrides, services.getenv)
	if err != nil {
		return fail(err)
	}
	model, err := services.newModel(config)
	if err != nil {
		return fail(fmt.Errorf("construct %s model: %w", config.Provider, err))
	}
	preflighter, err := services.newPreflighter(soundsPath)
	if err != nil {
		return fail(fmt.Errorf("construct preflighter: %w", err))
	}
	inventory, err := engine.LoadSoundInventory(soundsPath)
	if err != nil {
		return fail(err)
	}
	absoluteSource, source, err := loadCommandSource(services.invocationDir, sourcePath)
	if err != nil {
		return fail(err)
	}

	candidate, err := suggest.NewService(model, preflighter).Suggest(ctx, suggest.SuggestInput{
		Provider:    config.Provider,
		Model:       config.Model,
		Prompt:      request,
		SourcePath:  absoluteSource,
		SoundsPath:  soundsPath,
		Source:      source,
		Samples:     sortedSamples(inventory),
		Instruments: []string{"bass", "brass", "pluck"},
	})
	if err != nil {
		return fail(err)
	}
	return suggestionReadyMsg{candidate: candidate}
}

func formatStudioEvent(diagnostic engine.Diagnostic) string {
	revision := diagnostic.RevisionSHA256
	if len(revision) > 8 {
		revision = revision[:8]
	}
	if diagnostic.Bar == nil {
		return fmt.Sprintf("%s %s: %v", revision, diagnostic.Phase, diagnostic.Err)
	}
	return fmt.Sprintf("%s bar %d %s: %v", revision, *diagnostic.Bar, diagnostic.Phase, diagnostic.Err)
}

// studioFlags mirrors suggest's AI flags; config resolves lazily on first
// use so pure playback needs no provider setup.
type studioFlags struct {
	provider string
	model    string
	timeout  string
	sounds   string
	source   string
}

func parseStudioFlags(args []string) (studioFlags, error) {
	flags := flag.NewFlagSet("studio", flag.ContinueOnError)
	var parsed studioFlags
	flags.StringVar(&parsed.provider, "provider", "", "AI provider: openai, ollama, or openai-compatible")
	flags.StringVar(&parsed.model, "model", "", "provider model name")
	flags.StringVar(&parsed.timeout, "timeout", "", "provider request timeout")
	flags.StringVar(&parsed.sounds, "sounds", "sound/808", "sound inventory directory")
	if err := flags.Parse(args); err != nil {
		return parsed, fmt.Errorf("studio flags: %w", err)
	}
	if len(flags.Args()) != 1 {
		return parsed, errors.New("usage: basso studio [flags] <source.fnl>")
	}
	parsed.source = flags.Args()[0]
	return parsed, nil
}

// runStudioCommand plays the source exactly like play, feeding the cockpit
// from the observer seam; quitting the UI stops playback.
func runStudioCommand(ctx context.Context, args []string, deps commandDependencies) error {
	path, err := resolveFile(args)
	if err != nil {
		return err
	}

	playbackCtx, cancel := context.WithCancel(ctx)
	defer cancel()

	flags, err := parseStudioFlags(args)
	if err != nil {
		return err
	}
	soundsPath, err := absoluteFrom(deps.invocationDir, flags.sounds)
	if err != nil {
		return fmt.Errorf("resolve sounds path: %w", err)
	}

	model := newStudioModel(filepath.Base(flags.source))
	model.sourcePath = path
	model.services = deps.studioServices(ai.Overrides{
		Provider: flags.provider,
		Model:    flags.model,
		Timeout:  flags.timeout,
	}, soundsPath)
	program := deps.newStudioProgram(model, tea.WithAltScreen())

	observers := playbackObservers{
		onBar: func(bar, bpm, stepsPerBar int) {
			program.Send(barMsg{bar: bar, bpm: bpm, stepsPerBar: stepsPerBar})
		},
		onDiagnostic: func(diagnostic engine.Diagnostic) {
			program.Send(diagnosticMsg{diagnostic: diagnostic})
		},
	}
	done := make(chan error, 1)
	go func() {
		done <- playSource(playbackCtx, path, observers, deps.newProvider, deps.newSink)
	}()

	if _, err := program.Run(); err != nil {
		cancel()
		<-done
		return err
	}
	cancel()
	return <-done
}
