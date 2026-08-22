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
