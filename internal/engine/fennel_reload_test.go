package engine

import (
	"context"
	"os"
	"path/filepath"
	"testing"
	"time"
)

// fennelSourceSample builds a minimal Fennel pattern source whose single
// hit's :sample field is sample, so tests can tell which source a given
// Next call reflects.
func fennelSourceSample(sample string) string {
	return `
(fn pattern [bar]
  [{:step 0 :sample "` + sample + `" :velocity 1.0}])

pattern
`
}

// TestFennelProvider_ReloadAtBarBoundary verifies that setPendingSource
// (the fsnotify test hook) doesn't retroactively change a bar already
// computed, but does apply starting with the next Next call — bar-granular
// reload without any additional bar-boundary bookkeeping in Next itself.
func TestFennelProvider_ReloadAtBarBoundary(t *testing.T) {
	provider, err := New(fennelSourceSample("a.wav"))
	if err != nil {
		t.Fatalf("New() error = %v, want nil", err)
	}

	hits0, _, _, err := provider.Next(0)
	if err != nil {
		t.Fatalf("Next(0) error = %v, want nil", err)
	}
	if len(hits0) != 1 || hits0[0].Sample != "a.wav" {
		t.Fatalf("Next(0) hits = %+v, want single hit sample a.wav", hits0)
	}

	provider.setPendingSource(fennelSourceSample("b.wav"))

	hits1, _, _, err := provider.Next(1)
	if err != nil {
		t.Fatalf("Next(1) error = %v, want nil", err)
	}
	if len(hits1) != 1 || hits1[0].Sample != "b.wav" {
		t.Fatalf("Next(1) hits = %+v, want single hit sample b.wav (reload should have applied)", hits1)
	}
}

// TestFennelProvider_NoAudioRestartOnReload runs an Engine against a
// FennelProvider across a setPendingSource reload and asserts the fakeSink
// records exactly one Start call and zero Teardown calls throughout — the
// audio device is opened once and never reopened by a reload.
func TestFennelProvider_NoAudioRestartOnReload(t *testing.T) {
	provider, err := New(fennelSourceSample("a.wav"))
	if err != nil {
		t.Fatalf("New() error = %v, want nil", err)
	}

	sink := &fakeSink{}
	waited := make(chan time.Time)
	clk := newFakeClock(false)
	clk.waited = waited
	engine := &Engine{sink: sink, clock: clk}

	ctx, cancel := context.WithCancel(context.Background())
	done := make(chan error, 1)
	go func() { done <- engine.Run(ctx, provider) }()

	// Wait for bar 0 to be scheduled and Run to reach the bar wait.
	select {
	case <-waited:
	case <-time.After(2 * time.Second):
		t.Fatal("Run did not reach the bar wait after scheduling bar 0")
	}

	sink.mu.Lock()
	startCalls, teardownCalls := sink.startCalls, sink.teardownCalls
	sink.mu.Unlock()
	if startCalls != 1 {
		t.Errorf("startCalls (after bar 0) = %d, want 1", startCalls)
	}
	if teardownCalls != 0 {
		t.Errorf("teardownCalls (after bar 0) = %d, want 0", teardownCalls)
	}

	provider.setPendingSource(fennelSourceSample("b.wav"))

	// bar 0's pattern is bpm 120, stepsPerBar 16 (defaults), so advance by
	// that duration to release the bar wait and let Run request bar 1.
	stepDuration := time.Minute / time.Duration(120*4)
	barDuration := 16 * stepDuration
	clk.Advance(barDuration)

	select {
	case <-waited:
	case <-time.After(2 * time.Second):
		t.Fatal("Run did not reach the bar wait after scheduling bar 1")
	}

	sink.mu.Lock()
	startCalls, teardownCalls = sink.startCalls, sink.teardownCalls
	fires := append([]recordedFire(nil), sink.fires...)
	sink.mu.Unlock()
	if startCalls != 1 {
		t.Errorf("startCalls (after bar 1) = %d, want 1", startCalls)
	}
	if teardownCalls != 0 {
		t.Errorf("teardownCalls (after bar 1) = %d, want 0", teardownCalls)
	}
	if len(fires) != 2 {
		t.Fatalf("len(fires) = %d, want 2 (one per bar)", len(fires))
	}
	if fires[0].source != "a.wav" {
		t.Errorf("fires[0].source = %q, want a.wav (bar 0, before reload)", fires[0].source)
	}
	if fires[1].source != "b.wav" {
		t.Errorf("fires[1].source = %q, want b.wav (bar 1, after reload)", fires[1].source)
	}

	cancel()

	select {
	case err := <-done:
		if err == nil {
			t.Fatal("Run() error = nil, want context.Canceled")
		}
	case <-time.After(2 * time.Second):
		t.Fatal("Run did not return promptly after ctx cancellation")
	}

	sink.mu.Lock()
	defer sink.mu.Unlock()
	if sink.startCalls != 1 {
		t.Errorf("startCalls (final) = %d, want 1", sink.startCalls)
	}
	if sink.teardownCalls != 1 {
		t.Errorf("teardownCalls (final) = %d, want 1", sink.teardownCalls)
	}
}

// TestFennelProvider_RealFsnotify proves the real fsnotify integration:
// NewFromFile watches a real temp file, and a real os.WriteFile edit to it
// eventually (after the debounce) becomes visible to Next, without needing
// the setPendingSource test hook.
func TestFennelProvider_RealFsnotify(t *testing.T) {
	dir := t.TempDir()
	path := filepath.Join(dir, "pattern.fnl")
	if err := os.WriteFile(path, []byte(fennelSourceSample("a.wav")), 0o644); err != nil {
		t.Fatalf("os.WriteFile(initial) error = %v, want nil", err)
	}

	provider, err := NewFromFile(path)
	if err != nil {
		t.Fatalf("NewFromFile() error = %v, want nil", err)
	}
	defer provider.Close()

	hits0, _, _, err := provider.Next(0)
	if err != nil {
		t.Fatalf("Next(0) error = %v, want nil", err)
	}
	if len(hits0) != 1 || hits0[0].Sample != "a.wav" {
		t.Fatalf("Next(0) hits = %+v, want single hit sample a.wav", hits0)
	}

	if err := os.WriteFile(path, []byte(fennelSourceSample("b.wav")), 0o644); err != nil {
		t.Fatalf("os.WriteFile(updated) error = %v, want nil", err)
	}

	// Real debounce wait: this is the one test explicitly proving the real
	// fsnotify integration, so a real sleep here is expected and fine.
	time.Sleep(300 * time.Millisecond)

	hits1, _, _, err := provider.Next(1)
	if err != nil {
		t.Fatalf("Next(1) error = %v, want nil", err)
	}
	if len(hits1) != 1 || hits1[0].Sample != "b.wav" {
		t.Fatalf("Next(1) hits = %+v, want single hit sample b.wav (real fsnotify reload should have applied)", hits1)
	}
}
