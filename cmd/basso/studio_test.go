package main

import (
	"bytes"
	"context"
	"errors"
	"io"
	"strings"
	"testing"

	"github.com/charmbracelet/bubbletea"
	"github.com/nyelonong/basso/internal/engine"
)

// headlessProgram wraps a tea.Program configured for tests: no renderer, no
// signal handling, input piped from a keystroke string. Run() blocks until
// the model quits.
type headlessProgram struct {
	program *tea.Program
}

func (p headlessProgram) Run() (tea.Model, error) {
	return p.program.Run()
}

func (p headlessProgram) Send(msg tea.Msg) {
	p.program.Send(msg)
}

func newHeadlessProgram(keys string) func(tea.Model, ...tea.ProgramOption) programRunner {
	return func(model tea.Model, opts ...tea.ProgramOption) programRunner {
		all := append([]tea.ProgramOption{
			tea.WithInput(strings.NewReader(keys)),
			tea.WithoutRenderer(),
			tea.WithoutSignalHandler(),
		}, opts...)
		return headlessProgram{program: tea.NewProgram(model, all...)}
	}
}

// TestHelpCommand_ListsStudio verifies the single documented usage line for
// the new subcommand.
func TestHelpCommand_ListsStudio(t *testing.T) {
	var stdout bytes.Buffer
	if err := writeTopLevelHelp(&stdout); err != nil {
		t.Fatalf("writeTopLevelHelp() error = %v", err)
	}
	if !strings.Contains(stdout.String(), "basso studio <source.fnl>") {
		t.Errorf("help = %q, want basso studio usage line", stdout.String())
	}
}

// closingProvider decorates a closablePatternProvider, signalling Close.
type closingProvider struct {
	inner  closablePatternProvider
	closed chan struct{}
}

func (p *closingProvider) Next(bar int) ([]engine.Hit, int, int, error) {
	return p.inner.Next(bar)
}

func (p *closingProvider) Close() error {
	p.closed <- struct{}{}
	return p.inner.Close()
}

// TestRunStudioCommand_PlaysAndQuitsCleanly starts studio headless against a
// stub provider that errors after two bars, feeds it a quit keypress, and
// asserts playback started through the shared sink factory and tore down
// without touching files.
func TestRunStudioCommand_PlaysAndQuitsCleanly(t *testing.T) {
	sinkConstructed := false
	stopErr := errors.New("stop after second bar")
	providerClosed := make(chan struct{}, 1)

	deps := testCommandDependencies(t.TempDir(), io.Discard, io.Discard)
	deps.newSink = func() engine.AudioSink {
		sinkConstructed = true
		return newFakeSink()
	}
	deps.newProvider = func(path string, _ engine.DiagnosticReporter) (closablePatternProvider, error) {
		if !strings.HasSuffix(path, "pattern.fnl") {
			t.Errorf("provider path = %q, want pattern.fnl", path)
		}
		return &closingProvider{inner: &countingProvider{stops: 2, stopErr: stopErr}, closed: providerClosed}, nil
	}
	deps.newStudioProgram = newHeadlessProgram("q")

	err := runStudioCommand(context.Background(), []string{"pattern.fnl"}, deps)
	if err != nil {
		t.Fatalf("runStudioCommand() error = %v", err)
	}
	if !sinkConstructed {
		t.Error("audio sink was never constructed; studio did not play")
	}
	select {
	case <-providerClosed:
	default:
		t.Error("provider was not closed on quit")
	}
}

// TestRunStudioCommand_RequiresOneFile verifies argument validation matches
// play's contract before any UI or audio is created.
func TestRunStudioCommand_RequiresOneFile(t *testing.T) {
	deps := testCommandDependencies(t.TempDir(), io.Discard, io.Discard)
	called := false
	deps.newProvider = func(string, engine.DiagnosticReporter) (closablePatternProvider, error) {
		called = true
		return nil, nil
	}

	for _, args := range [][]string{{}, {"a.fnl", "b.fnl"}} {
		if err := runStudioCommand(context.Background(), args, deps); err == nil {
			t.Errorf("runStudioCommand(%v) error = nil, want error", args)
		}
	}
	if called {
		t.Error("provider constructor was called for invalid args")
	}
}

func studioDiagnostic(revision string, bar int, phase engine.DiagnosticPhase, message string) engine.Diagnostic {
	return engine.Diagnostic{
		RevisionSHA256: revision,
		Bar:            &bar,
		Phase:          phase,
		Err:            errors.New(message),
	}
}

// TestStudioModel_BarAdvancesInView proves barMsg drives the status line.
func TestStudioModel_BarAdvancesInView(t *testing.T) {
	model := newStudioModel("basic-groove.fnl")
	if strings.Contains(model.View(), "bar ") {
		t.Fatalf("initial view shows a bar before playback: %q", model.View())
	}

	updated, _ := model.Update(barMsg{bar: 5, bpm: 130, stepsPerBar: 16})
	view := updated.(studioModel).View()
	for _, want := range []string{"bar 5 bpm 130 steps 16", "basic-groove.fnl"} {
		if !strings.Contains(view, want) {
			t.Errorf("view = %q, want substring %q", view, want)
		}
	}
}

// TestStudioModel_DiagnosticsRenderAsCappedEvents proves reload diagnostics
// become an on-screen event log that keeps only the most recent entries.
func TestStudioModel_DiagnosticsRenderAsCappedEvents(t *testing.T) {
	var model tea.Model = newStudioModel("pattern.fnl")
	first := studioDiagnostic(strings.Repeat("a", 64), 1, engine.DiagnosticPhaseValidate, "bad hit")
	second := studioDiagnostic(strings.Repeat("b", 64), 2, engine.DiagnosticPhaseWatch, "stale revision")

	model, _ = model.Update(diagnosticMsg{diagnostic: first})
	model, _ = model.Update(diagnosticMsg{diagnostic: second})
	for i := 0; i < maxStudioEvents; i++ {
		model, _ = model.Update(barMsg{bar: i, bpm: 120, stepsPerBar: 16})
	}

	view := model.(studioModel).View()
	if !strings.Contains(view, "bar 1 validate: bad hit") || !strings.Contains(view, "bar 2 watch: stale revision") {
		t.Errorf("view = %q, want both diagnostic events rendered", view)
	}
	if !strings.Contains(view, strings.Repeat("a", 8)) || !strings.Contains(view, strings.Repeat("b", 8)) {
		t.Errorf("view = %q, want short revision prefixes", view)
	}
	if got := model.(studioModel).diagnosticsObserved; got != 2 {
		t.Errorf("diagnosticsObserved = %d, want 2", got)
	}
}

// TestStudioModel_EventLogKeepsMostRecent proves the cap drops oldest events.
func TestStudioModel_EventLogKeepsMostRecent(t *testing.T) {
	var model tea.Model = newStudioModel("pattern.fnl")
	total := maxStudioEvents + 3
	for i := 0; i < total; i++ {
		model, _ = model.Update(diagnosticMsg{diagnostic: studioDiagnostic(
			strings.Repeat(string(rune('a'+i%26)), 64), i, engine.DiagnosticPhaseWatch, "tick",
		)})
	}

	m := model.(studioModel)
	if len(m.events) != maxStudioEvents {
		t.Fatalf("events = %d, want capped at %d", len(m.events), maxStudioEvents)
	}
	if !strings.Contains(m.View(), "watch: tick") {
		t.Error("view lost the most recent event")
	}
}

// TestStudioModel_QuitAndPlaybackDone proves quit keys and playback failure
// both end the program.
func TestStudioModel_QuitAndPlaybackDone(t *testing.T) {
	model := newStudioModel("pattern.fnl")

	q := tea.KeyMsg{Type: tea.KeyRunes, Runes: []rune{'q'}}
	if _, cmd := model.Update(q); cmd == nil {
		t.Error("q key produced no quit command")
	}
	ctrlC := tea.KeyMsg{Type: tea.KeyCtrlC}
	if _, cmd := model.Update(ctrlC); cmd == nil {
		t.Error("ctrl+c produced no quit command")
	}

	failed, cmd := model.Update(playbackDoneMsg{err: errors.New("device gone")})
	if cmd == nil {
		t.Error("playbackDoneMsg produced no quit command")
	}
	m := failed.(studioModel)
	if !strings.Contains(m.View(), "playback stopped: device gone") {
		t.Errorf("view = %q, want playback failure", m.View())
	}
}
