package main

import (
	"context"
	"strings"
	"sync"
	"testing"
	"time"

	tea "github.com/charmbracelet/bubbletea"
	"github.com/nyelonong/basso/internal/engine"
)

type recordingTransport struct {
	state studioTransportState
	calls []string
}

func (transport *recordingTransport) TogglePause() studioTransportState {
	transport.calls = append(transport.calls, "toggle")
	if transport.state == transportPlaying {
		transport.state = transportPaused
	} else if transport.state == transportPaused {
		transport.state = transportPlaying
	}
	return transport.state
}

func (transport *recordingTransport) Stop() studioTransportState {
	transport.calls = append(transport.calls, "stop")
	transport.state = transportStopped
	return transport.state
}

func (transport *recordingTransport) Play() studioTransportState {
	transport.calls = append(transport.calls, "play")
	if transport.state == transportStopped {
		transport.state = transportPlaying
	}
	return transport.state
}

func TestStudioTransport_KeysAndStatus(t *testing.T) {
	control := &recordingTransport{state: transportPlaying}
	model := newStudioModel("pattern.fnl")
	model.transport = control
	model.transportState = transportPlaying

	updated, _ := model.Update(tea.KeyMsg{Type: tea.KeySpace})
	model = updated.(studioModel)
	if model.transportState != transportPaused || control.calls[0] != "toggle" {
		t.Fatalf("space state/calls = %s/%v, want paused/[toggle]", model.transportState, control.calls)
	}
	if view := model.View(); !strings.Contains(view, "transport paused") {
		t.Fatalf("View() = %q, want transport paused", view)
	}

	updated, _ = model.Update(tea.KeyMsg{Type: tea.KeySpace})
	model = updated.(studioModel)
	updated, _ = model.Update(tea.KeyMsg{Type: tea.KeyRunes, Runes: []rune{'x'}})
	model = updated.(studioModel)
	if model.transportState != transportStopped {
		t.Fatalf("x state = %s, want stopped", model.transportState)
	}
	updated, _ = model.Update(tea.KeyMsg{Type: tea.KeyRunes, Runes: []rune{'p'}})
	model = updated.(studioModel)
	if model.transportState != transportPlaying {
		t.Fatalf("p state = %s, want playing", model.transportState)
	}
}

func TestStudioTransport_KeysBelongToFocusedPrompt(t *testing.T) {
	control := &recordingTransport{state: transportPlaying}
	model := newStudioModel("pattern.fnl")
	model.transport = control
	model.transportState = transportPlaying

	updated, _ := model.Update(suggestPromptMsg{})
	model = updated.(studioModel)
	updated, _ = model.Update(tea.KeyMsg{Type: tea.KeySpace, Runes: []rune{' '}})
	model = updated.(studioModel)
	updated, _ = model.Update(tea.KeyMsg{Type: tea.KeyRunes, Runes: []rune{'x'}})
	model = updated.(studioModel)

	if model.prompt.Value() != " x" {
		t.Fatalf("prompt value = %q, want %q", model.prompt.Value(), " x")
	}
	if len(control.calls) != 0 {
		t.Fatalf("transport calls while prompting = %v, want none", control.calls)
	}
}

type transportTestProvider struct {
	bars   chan int
	mu     sync.Mutex
	closed int
}

func (provider *transportTestProvider) Next(bar int) ([]engine.Hit, int, int, error) {
	provider.bars <- bar
	return nil, 400, 1, nil
}

func (provider *transportTestProvider) Close() error {
	provider.mu.Lock()
	provider.closed++
	provider.mu.Unlock()
	return nil
}

type transportTestSink struct {
	mu        sync.Mutex
	starts    int
	teardowns int
}

func (sink *transportTestSink) Start() {
	sink.mu.Lock()
	sink.starts++
	sink.mu.Unlock()
}

func (sink *transportTestSink) Teardown() {
	sink.mu.Lock()
	sink.teardowns++
	sink.mu.Unlock()
}

func (*transportTestSink) SetFire(string, time.Duration, time.Duration, float64, float64) {}
func (*transportTestSink) SetFireNote(string, string, time.Duration, time.Duration, float64, float64) {
}

func waitTransportBar(t *testing.T, bars <-chan int) int {
	t.Helper()
	select {
	case bar := <-bars:
		return bar
	case <-time.After(2 * time.Second):
		t.Fatal("transport did not produce a bar before timeout")
		return -1
	}
}

func TestStudioTransport_PauseContinuesAndStopResetsWithoutDeviceRestart(t *testing.T) {
	provider := &transportTestProvider{bars: make(chan int, 16)}
	sink := &transportTestSink{}
	transport, err := newStudioTransport(
		context.Background(),
		"pattern.fnl",
		playbackObservers{},
		func(string, engine.DiagnosticReporter) (closablePatternProvider, error) { return provider, nil },
		func() engine.AudioSink { return sink },
		nil,
	)
	if err != nil {
		t.Fatalf("newStudioTransport() error = %v", err)
	}
	transport.Start()
	if bar := waitTransportBar(t, provider.bars); bar != 0 {
		t.Fatalf("first bar = %d, want 0", bar)
	}

	transport.TogglePause()
	time.Sleep(80 * time.Millisecond)
	select {
	case bar := <-provider.bars:
		t.Fatalf("bar %d scheduled while paused", bar)
	default:
	}
	transport.TogglePause()
	if bar := waitTransportBar(t, provider.bars); bar != 1 {
		t.Fatalf("resumed bar = %d, want 1", bar)
	}

	transport.Stop()
	transport.Play()
	if bar := waitTransportBar(t, provider.bars); bar != 0 {
		t.Fatalf("post-stop bar = %d, want 0", bar)
	}
	if err := transport.Close(); err != nil {
		t.Fatalf("Close() error = %v", err)
	}

	sink.mu.Lock()
	starts, teardowns := sink.starts, sink.teardowns
	sink.mu.Unlock()
	if starts != 1 || teardowns != 1 {
		t.Fatalf("sink lifecycle = (%d starts, %d teardowns), want (1, 1)", starts, teardowns)
	}
	provider.mu.Lock()
	closed := provider.closed
	provider.mu.Unlock()
	if closed != 1 {
		t.Fatalf("provider Close calls = %d, want 1", closed)
	}
}
