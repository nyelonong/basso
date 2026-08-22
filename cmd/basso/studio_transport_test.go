package main

import (
	"context"
	"os"
	"path/filepath"
	"strings"
	"sync"
	"testing"
	"time"

	tea "github.com/charmbracelet/bubbletea"
	"github.com/nyelonong/basso/internal/engine"
)

type recordingTransport struct {
	state      studioTransportState
	calls      []string
	armedPath  string
	armErr     error
	commitErr  error
	releaseErr error
	onRelease  func()
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

func (transport *recordingTransport) ArmCandidate(path string) error {
	transport.calls = append(transport.calls, "arm")
	transport.armedPath = path
	return transport.armErr
}

func (transport *recordingTransport) CommitCandidate() error {
	transport.calls = append(transport.calls, "commit")
	if transport.onRelease != nil {
		transport.onRelease()
	}
	return transport.commitErr
}

func (transport *recordingTransport) ReleaseCandidate() error {
	transport.calls = append(transport.calls, "release")
	if transport.onRelease != nil {
		transport.onRelease()
	}
	return transport.releaseErr
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
	updated, stopCommand := model.Update(tea.KeyMsg{Type: tea.KeyRunes, Runes: []rune{'x'}})
	model = updated.(studioModel)
	if model.transportState != transportStopping || stopCommand == nil {
		t.Fatalf("x state/command = %s/%v, want stopping/non-nil", model.transportState, stopCommand)
	}
	updated, _ = model.Update(stopCommand())
	model = updated.(studioModel)
	if model.transportState != transportStopped {
		t.Fatalf("stop completion state = %s, want stopped", model.transportState)
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

type refreshableTransportProvider struct {
	*transportTestProvider
	refreshes int
}

func (provider *refreshableTransportProvider) Refresh() error {
	provider.mu.Lock()
	provider.refreshes++
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

	stopStarted := time.Now()
	transport.Stop()
	if elapsed := time.Since(stopStarted); elapsed < 25*time.Millisecond {
		t.Fatalf("Stop returned after %v, want current bar boundary first", elapsed)
	}
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

type namedPatternProvider struct {
	name string
	bars []int
}

func (provider *namedPatternProvider) Next(bar int) ([]engine.Hit, int, int, error) {
	provider.bars = append(provider.bars, bar)
	return []engine.Hit{{Step: 0, Sample: provider.name, Velocity: 1}}, 120, 16, nil
}

func TestStudioTransport_CommitRefreshesRealBeforeReleasingCandidate(t *testing.T) {
	real := &refreshableTransportProvider{transportTestProvider: &transportTestProvider{bars: make(chan int, 1)}}
	candidate := &transportTestProvider{bars: make(chan int, 1)}
	transport, err := newStudioTransport(
		context.Background(),
		"real.fnl",
		playbackObservers{},
		func(path string, _ engine.DiagnosticReporter) (closablePatternProvider, error) {
			if path == "candidate.fnl" {
				return candidate, nil
			}
			return real, nil
		},
		func() engine.AudioSink { return &transportTestSink{} },
		nil,
	)
	if err != nil {
		t.Fatal(err)
	}
	if err := transport.ArmCandidate("candidate.fnl"); err != nil {
		t.Fatal(err)
	}
	if err := transport.CommitCandidate(); err != nil {
		t.Fatalf("CommitCandidate() error = %v", err)
	}
	real.mu.Lock()
	refreshes := real.refreshes
	real.mu.Unlock()
	candidate.mu.Lock()
	closed := candidate.closed
	candidate.mu.Unlock()
	if refreshes != 1 || closed != 1 {
		t.Fatalf("commit lifecycle = (%d refreshes, %d candidate closes), want (1, 1)", refreshes, closed)
	}
	if err := transport.Close(); err != nil {
		t.Fatal(err)
	}
}

func TestStudioTransport_ReleaseClosesWatcherBeforeDelete(t *testing.T) {
	dir := t.TempDir()
	realPath := filepath.Join(dir, "real.fnl")
	candidatePath := filepath.Join(dir, "candidate.fnl")
	source := []byte("(fn pattern [bar] [])\npattern\n")
	for _, path := range []string{realPath, candidatePath} {
		if err := os.WriteFile(path, source, 0o600); err != nil {
			t.Fatal(err)
		}
	}
	diagnostics := make(chan engine.Diagnostic, 4)
	constructor := func(path string, reporter engine.DiagnosticReporter) (closablePatternProvider, error) {
		return engine.NewFromFile(path, engine.NewEvaluator(engine.SoundInventory{}, 250*time.Millisecond), reporter)
	}
	transport, err := newStudioTransport(
		context.Background(),
		realPath,
		playbackObservers{onDiagnostic: func(diagnostic engine.Diagnostic) { diagnostics <- diagnostic }},
		constructor,
		func() engine.AudioSink { return &transportTestSink{} },
		nil,
	)
	if err != nil {
		t.Fatalf("newStudioTransport() error = %v", err)
	}
	defer transport.Close()
	if err := transport.ArmCandidate(candidatePath); err != nil {
		t.Fatalf("ArmCandidate() error = %v", err)
	}
	if err := transport.ReleaseCandidate(); err != nil {
		t.Fatalf("ReleaseCandidate() error = %v", err)
	}
	if err := os.Remove(candidatePath); err != nil {
		t.Fatalf("remove released candidate: %v", err)
	}

	select {
	case diagnostic := <-diagnostics:
		t.Fatalf("released candidate emitted diagnostic: %+v", diagnostic)
	case <-time.After(250 * time.Millisecond):
	}
}

func TestStudioTransport_ArmCandidateSwitchesNextBar(t *testing.T) {
	real := &transportTestProvider{bars: make(chan int, 16)}
	candidate := &transportTestProvider{bars: make(chan int, 16)}
	transport, err := newStudioTransport(
		context.Background(),
		"real.fnl",
		playbackObservers{},
		func(path string, _ engine.DiagnosticReporter) (closablePatternProvider, error) {
			if path == "candidate.fnl" {
				return candidate, nil
			}
			return real, nil
		},
		func() engine.AudioSink { return &transportTestSink{} },
		nil,
	)
	if err != nil {
		t.Fatalf("newStudioTransport() error = %v", err)
	}
	transport.Start()
	if bar := waitTransportBar(t, real.bars); bar != 0 {
		t.Fatalf("real bar = %d, want 0", bar)
	}
	if err := transport.ArmCandidate("candidate.fnl"); err != nil {
		t.Fatalf("ArmCandidate() error = %v", err)
	}
	if bar := waitTransportBar(t, candidate.bars); bar != 1 {
		t.Fatalf("candidate bar = %d, want 1", bar)
	}
	if err := transport.Close(); err != nil {
		t.Fatalf("Close() error = %v", err)
	}
}

func TestSwitchingProvider_ArmsCandidateAtNextBar(t *testing.T) {
	real := &namedPatternProvider{name: "real.wav"}
	candidate := &namedPatternProvider{name: "candidate.wav"}
	provider := newSwitchingProvider(real)

	hits, _, _, _ := provider.Next(0)
	if hits[0].Sample != "real.wav" {
		t.Fatalf("bar 0 sample = %q, want real.wav", hits[0].Sample)
	}
	switched, err := provider.Arm(candidate, false)
	if err != nil {
		t.Fatalf("Arm() error = %v", err)
	}
	select {
	case <-switched:
		t.Fatal("candidate switched before the next bar")
	default:
	}

	hits, _, _, _ = provider.Next(1)
	if hits[0].Sample != "candidate.wav" {
		t.Fatalf("bar 1 sample = %q, want candidate.wav", hits[0].Sample)
	}
	select {
	case <-switched:
	default:
		t.Fatal("candidate switch did not complete at next bar")
	}
	if len(candidate.bars) != 1 || candidate.bars[0] != 1 {
		t.Fatalf("candidate bars = %v, want [1]", candidate.bars)
	}
}

func TestSwitchingProvider_ArmsImmediatelyWhileNotPlaying(t *testing.T) {
	real := &namedPatternProvider{name: "real.wav"}
	candidate := &namedPatternProvider{name: "candidate.wav"}
	provider := newSwitchingProvider(real)

	switched, err := provider.Arm(candidate, true)
	if err != nil {
		t.Fatalf("Arm() error = %v", err)
	}
	select {
	case <-switched:
	default:
		t.Fatal("immediate candidate switch stayed pending")
	}
	hits, _, _, _ := provider.Next(0)
	if hits[0].Sample != "candidate.wav" {
		t.Fatalf("first sample = %q, want candidate.wav", hits[0].Sample)
	}
	if _, err := provider.Arm(&namedPatternProvider{name: "other.wav"}, true); err == nil {
		t.Fatal("second Arm() error = nil, want one-candidate guard")
	}
}

func TestSwitchingProvider_ReturnsToRealAtNextBar(t *testing.T) {
	real := &namedPatternProvider{name: "real.wav"}
	candidate := &namedPatternProvider{name: "candidate.wav"}
	provider := newSwitchingProvider(real)
	if _, err := provider.Arm(candidate, true); err != nil {
		t.Fatalf("Arm() error = %v", err)
	}
	provider.Next(0)

	switched := provider.UseReal(false)
	select {
	case <-switched:
		t.Fatal("real provider switched before next bar")
	default:
	}
	hits, _, _, _ := provider.Next(1)
	if hits[0].Sample != "real.wav" {
		t.Fatalf("bar 1 sample = %q, want real.wav", hits[0].Sample)
	}
	select {
	case <-switched:
	default:
		t.Fatal("real switch did not complete at next bar")
	}
	provider.ClearCandidate()
	if _, err := provider.Arm(&namedPatternProvider{name: "next.wav"}, true); err != nil {
		t.Fatalf("Arm() after clear error = %v", err)
	}
}
