// basso studio: a full-screen cockpit over playback — live bar/BPM/steps
// status plus the AI candidate review loop. Playback reuses the exact play
// machinery; the UI only observes it.
package main

import (
	"context"
	"fmt"
	"path/filepath"
	"strings"

	"github.com/charmbracelet/bubbletea"
	"github.com/nyelonong/basso/internal/engine"
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

type studioModel struct {
	sourceName          string
	bar                 int
	bpm                 int
	stepsPerBar         int
	played              bool
	playbackErr         string
	diagnosticsObserved int
	events              []string
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
		}
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
	out.WriteString("\nq quit\n")
	return out.String()
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

// runStudioCommand plays the source exactly like play, feeding the cockpit
// from the observer seam; quitting the UI stops playback.
func runStudioCommand(ctx context.Context, args []string, deps commandDependencies) error {
	path, err := resolveFile(args)
	if err != nil {
		return err
	}

	playbackCtx, cancel := context.WithCancel(ctx)
	defer cancel()

	model := newStudioModel(filepath.Base(path))
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
