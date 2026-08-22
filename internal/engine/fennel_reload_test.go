package engine

import (
	"context"
	"crypto/sha256"
	"fmt"
	"os"
	"path/filepath"
	"strings"
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

func fennelPaletteOverlap(count int) string {
	var source strings.Builder
	source.WriteString("(fn pattern [bar]\n  [")
	for range count {
		source.WriteString(`
   {:step 0 :note "C3" :instrument "pad" :length 4}`)
	}
	source.WriteString("])\n\npattern\n")
	return source.String()
}

func TestNewFromFile_RejectsInvalidInitialSourceBeforeSinkStart(t *testing.T) {
	path := filepath.Join(t.TempDir(), "pattern.fnl")
	if err := os.WriteFile(path, []byte("(fn pattern [bar]"), 0o644); err != nil {
		t.Fatalf("os.WriteFile() error = %v, want nil", err)
	}

	sink := &fakeSink{}
	provider, err := NewFromFile(path, NewEvaluator(fennelProviderTestInventory(), legacyEvaluationTimeout), nil)
	if err == nil {
		defer provider.Close()
		engine := &Engine{sink: sink, clock: newFakeClock(false)}
		_ = engine.Run(context.Background(), provider)
	}

	if err == nil {
		t.Fatal("NewFromFile() error = nil, want invalid initial source error")
	}
	sink.mu.Lock()
	defer sink.mu.Unlock()
	if sink.startCalls != 0 {
		t.Fatalf("sink.Start calls = %d, want 0", sink.startCalls)
	}
}

// TestFennelProvider_ReloadAtBarBoundary verifies that setPendingSource
// (the fsnotify test hook) doesn't retroactively change a bar already
// computed, but does apply starting with the next Next call — bar-granular
// reload without any additional bar-boundary bookkeeping in Next itself.
func TestFennelProvider_ReloadAtBarBoundary(t *testing.T) {
	provider, err := New(
		fennelSourceSample("a.wav"),
		NewEvaluator(SoundInventory{"a.wav": {}, "b.wav": {}}, legacyEvaluationTimeout),
		nil,
	)
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

func TestFennelProvider_InvalidPendingKeepsActive(t *testing.T) {
	evaluator := NewEvaluator(SoundInventory{"a.wav": {}}, legacyEvaluationTimeout)
	provider, err := New(fennelSourceSample("a.wav"), evaluator, nil)
	if err != nil {
		t.Fatalf("New() error = %v, want nil", err)
	}

	provider.setPendingSource(fennelSourceSample("missing.wav"))

	hits, _, _, err := provider.Next(1)
	if err != nil {
		t.Fatalf("Next(1) error = %v, want nil", err)
	}
	if len(hits) != 1 || hits[0].Sample != "a.wav" {
		t.Fatalf("Next(1) hits = %+v, want active a.wav hit", hits)
	}
}

func TestFennelProvider_ValidAfterRejectedEditActivatesNextBar(t *testing.T) {
	inventory := SoundInventory{"a.wav": {}, "b.wav": {}}
	var diagnostics []Diagnostic
	provider, err := New(
		fennelSourceSample("a.wav"),
		NewEvaluator(inventory, legacyEvaluationTimeout),
		func(diagnostic Diagnostic) {
			diagnostics = append(diagnostics, diagnostic)
		},
	)
	if err != nil {
		t.Fatalf("New() error = %v, want nil", err)
	}

	invalid := fennelSourceSample("missing.wav")
	provider.setPendingSource(invalid)
	if _, _, _, err := provider.Next(1); err != nil {
		t.Fatalf("Next(1) error = %v, want nil", err)
	}

	provider.setPendingSource(fennelSourceSample("b.wav"))
	hits, _, _, err := provider.Next(2)
	if err != nil {
		t.Fatalf("Next(2) error = %v, want nil", err)
	}
	if len(hits) != 1 || hits[0].Sample != "b.wav" {
		t.Fatalf("Next(2) hits = %+v, want newly active b.wav hit", hits)
	}

	if len(diagnostics) != 1 {
		t.Fatalf("len(diagnostics) = %d, want 1", len(diagnostics))
	}
	wantRevision := fmt.Sprintf("%x", sha256.Sum256([]byte(invalid)))
	if diagnostics[0].RevisionSHA256 != wantRevision {
		t.Errorf("diagnostic revision = %q, want %q", diagnostics[0].RevisionSHA256, wantRevision)
	}
	if diagnostics[0].Bar == nil || *diagnostics[0].Bar != 1 {
		t.Errorf("diagnostic bar = %v, want 1", diagnostics[0].Bar)
	}
	if diagnostics[0].Phase != DiagnosticPhaseValidate {
		t.Errorf("diagnostic phase = %q, want %q", diagnostics[0].Phase, DiagnosticPhaseValidate)
	}
	if diagnostics[0].Err == nil {
		t.Error("diagnostic error = nil, want underlying validation error")
	}
}

func TestFennelProvider_LaterActiveFailureRepeatsLastGoodBar(t *testing.T) {
	source := `
(fn pattern [bar]
  (if (= bar 0)
      [{:step 0 :sample "a.wav" :velocity 0.75}]
      (error "later failure")))

pattern
`
	var diagnostics []Diagnostic
	provider, err := New(
		source,
		NewEvaluator(SoundInventory{"a.wav": {}}, legacyEvaluationTimeout),
		func(diagnostic Diagnostic) {
			diagnostics = append(diagnostics, diagnostic)
		},
	)
	if err != nil {
		t.Fatalf("New() error = %v, want nil", err)
	}

	hits, bpm, steps, err := provider.Next(0)
	if err != nil {
		t.Fatalf("Next(0) error = %v, want nil", err)
	}
	hits[0].Sample = "mutated.wav"
	hits[0].Velocity = 0

	fallback, fallbackBPM, fallbackSteps, err := provider.Next(1)
	if err != nil {
		t.Fatalf("Next(1) error = %v, want nil fallback", err)
	}
	if len(fallback) != 1 {
		t.Fatalf("len(fallback) = %d, want 1", len(fallback))
	}
	if fallback[0].Sample != "a.wav" || fallback[0].Velocity != 0.75 {
		t.Errorf("fallback hit = %+v, want defensive copy of last good hit", fallback[0])
	}
	if fallbackBPM != bpm || fallbackSteps != steps {
		t.Errorf(
			"fallback tempo = (%d, %d), want (%d, %d)",
			fallbackBPM,
			fallbackSteps,
			bpm,
			steps,
		)
	}
	if len(diagnostics) != 1 || diagnostics[0].Phase != DiagnosticPhaseEvaluate {
		t.Fatalf("diagnostics = %+v, want one evaluate diagnostic", diagnostics)
	}
}

func TestFennelProvider_TimeoutRepeatsLastGoodBar(t *testing.T) {
	source := `
(fn pattern [bar]
  (if (= bar 0)
      [{:step 0 :sample "a.wav" :velocity 1.0}]
      (while true)))

pattern
`
	var diagnostics []Diagnostic
	provider, err := New(
		source,
		NewEvaluator(SoundInventory{"a.wav": {}}, 250*time.Millisecond),
		func(diagnostic Diagnostic) {
			diagnostics = append(diagnostics, diagnostic)
		},
	)
	if err != nil {
		t.Fatalf("New() error = %v, want nil", err)
	}

	for bar := 1; bar <= 2; bar++ {
		hits, _, _, err := provider.Next(bar)
		if err != nil {
			t.Fatalf("Next(%d) error = %v, want nil fallback", bar, err)
		}
		if len(hits) != 1 || hits[0].Sample != "a.wav" {
			t.Fatalf("Next(%d) hits = %+v, want last-good a.wav hit", bar, hits)
		}
	}

	if len(diagnostics) != 1 {
		t.Fatalf("len(diagnostics) = %d, want one deduplicated timeout", len(diagnostics))
	}
	if diagnostics[0].Phase != DiagnosticPhaseTimeout {
		t.Fatalf("diagnostic phase = %q, want %q", diagnostics[0].Phase, DiagnosticPhaseTimeout)
	}
}

// TestFennelProvider_NoAudioRestartAcrossPaletteRejectAndAccept runs an
// Engine through one rejected edit and one accepted edit while the same fake
// sink remains open.
func TestFennelProvider_NoAudioRestartAcrossPaletteRejectAndAccept(t *testing.T) {
	provider, err := New(
		fennelSourceSample("a.wav"),
		NewEvaluator(SoundInventory{"a.wav": {}, "b.wav": {}}, legacyEvaluationTimeout),
		nil,
	)
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

	provider.setPendingSource(fennelPaletteOverlap(9))

	// bar 0's pattern is bpm 120, stepsPerBar 16 (defaults), so advance by
	// that duration to release the bar wait and let Run request bar 1.
	stepDuration := time.Minute / time.Duration(120*4)
	barDuration := 16 * stepDuration
	clk.Advance(barDuration)

	select {
	case <-waited:
	case <-time.After(2 * time.Second):
		t.Fatal("Run did not reach the bar wait after rejecting bar 1 edit")
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
		t.Fatalf("len(fires) = %d, want 2 after rejected edit", len(fires))
	}
	if fires[0].source != "a.wav" {
		t.Errorf("fires[0].source = %q, want a.wav (bar 0, before reload)", fires[0].source)
	}
	if fires[1].source != "a.wav" {
		t.Errorf("fires[1].source = %q, want a.wav (bar 1, rejected edit)", fires[1].source)
	}

	provider.setPendingSource(fennelSourceSample("b.wav"))
	clk.Advance(barDuration)
	select {
	case <-waited:
	case <-time.After(2 * time.Second):
		t.Fatal("Run did not reach the bar wait after accepting bar 2 edit")
	}

	sink.mu.Lock()
	startCalls, teardownCalls = sink.startCalls, sink.teardownCalls
	fires = append([]recordedFire(nil), sink.fires...)
	sink.mu.Unlock()
	if startCalls != 1 || teardownCalls != 0 {
		t.Errorf(
			"sink lifecycle after accept = (%d starts, %d teardowns), want (1, 0)",
			startCalls,
			teardownCalls,
		)
	}
	if len(fires) != 3 || fires[2].source != "b.wav" {
		t.Fatalf("fires after accept = %+v, want third fire from b.wav", fires)
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

	provider, err := NewFromFile(
		path,
		NewEvaluator(SoundInventory{"a.wav": {}, "b.wav": {}}, legacyEvaluationTimeout),
		nil,
	)
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

func TestFennelProvider_RefreshStagesAcceptedSourceImmediately(t *testing.T) {
	dir := t.TempDir()
	path := filepath.Join(dir, "pattern.fnl")
	if err := os.WriteFile(path, []byte(fennelSourceSample("a.wav")), 0o644); err != nil {
		t.Fatal(err)
	}
	provider, err := NewFromFile(
		path,
		NewEvaluator(SoundInventory{"a.wav": {}, "b.wav": {}}, legacyEvaluationTimeout),
		nil,
	)
	if err != nil {
		t.Fatal(err)
	}
	defer provider.Close()

	if err := os.WriteFile(path, []byte(fennelSourceSample("b.wav")), 0o644); err != nil {
		t.Fatal(err)
	}
	if err := provider.Refresh(); err != nil {
		t.Fatalf("Refresh() error = %v", err)
	}
	hits, _, _, err := provider.Next(1)
	if err != nil {
		t.Fatalf("Next(1) error = %v", err)
	}
	if len(hits) != 1 || hits[0].Sample != "b.wav" {
		t.Fatalf("Next(1) hits = %+v, want immediate b.wav revision", hits)
	}
}

func TestFennelProvider_WatchesAtomicReplacement(t *testing.T) {
	dir := t.TempDir()
	path := filepath.Join(dir, "pattern.fnl")
	if err := os.WriteFile(path, []byte(fennelSourceSample("a.wav")), 0o644); err != nil {
		t.Fatalf("os.WriteFile(initial) error = %v, want nil", err)
	}

	provider, err := NewFromFile(
		path,
		NewEvaluator(SoundInventory{"a.wav": {}, "b.wav": {}}, legacyEvaluationTimeout),
		nil,
	)
	if err != nil {
		t.Fatalf("NewFromFile() error = %v, want nil", err)
	}
	defer provider.Close()
	watchList := provider.watcher.WatchList()
	if len(watchList) != 1 || filepath.Clean(watchList[0]) != filepath.Clean(dir) {
		t.Fatalf("watch list = %v, want cleaned parent directory %q", watchList, dir)
	}

	replacement := filepath.Join(dir, "replacement.fnl")
	if err := os.WriteFile(replacement, []byte(fennelSourceSample("b.wav")), 0o644); err != nil {
		t.Fatalf("os.WriteFile(replacement) error = %v, want nil", err)
	}
	if err := os.Rename(replacement, path); err != nil {
		t.Fatalf("os.Rename() error = %v, want nil", err)
	}

	deadline := time.Now().Add(2 * time.Second)
	for bar := 1; time.Now().Before(deadline); bar++ {
		hits, _, _, err := provider.Next(bar)
		if err != nil {
			t.Fatalf("Next(%d) error = %v, want nil", bar, err)
		}
		if len(hits) == 1 && hits[0].Sample == "b.wav" {
			return
		}
		time.Sleep(20 * time.Millisecond)
	}
	t.Fatal("atomic replacement was not observed before deadline")
}

func TestFennelProvider_RemoveKeepsActive(t *testing.T) {
	dir := t.TempDir()
	path := filepath.Join(dir, "pattern.fnl")
	if err := os.WriteFile(path, []byte(fennelSourceSample("a.wav")), 0o644); err != nil {
		t.Fatalf("os.WriteFile(initial) error = %v, want nil", err)
	}

	diagnostics := make(chan Diagnostic, 4)
	provider, err := NewFromFile(
		path,
		NewEvaluator(SoundInventory{"a.wav": {}, "b.wav": {}}, legacyEvaluationTimeout),
		func(diagnostic Diagnostic) {
			diagnostics <- diagnostic
		},
	)
	if err != nil {
		t.Fatalf("NewFromFile() error = %v, want nil", err)
	}
	defer provider.Close()

	if err := os.Remove(path); err != nil {
		t.Fatalf("os.Remove() error = %v, want nil", err)
	}
	select {
	case diagnostic := <-diagnostics:
		if diagnostic.Phase != DiagnosticPhaseWatch {
			t.Fatalf("remove diagnostic phase = %q, want %q", diagnostic.Phase, DiagnosticPhaseWatch)
		}
		if diagnostic.Bar != nil {
			t.Fatalf("remove diagnostic bar = %v, want nil", diagnostic.Bar)
		}
	case <-time.After(2 * time.Second):
		t.Fatal("remove did not produce a watch diagnostic")
	}

	hits, _, _, err := provider.Next(1)
	if err != nil {
		t.Fatalf("Next(1) after remove error = %v, want nil", err)
	}
	if len(hits) != 1 || hits[0].Sample != "a.wav" {
		t.Fatalf("Next(1) hits = %+v, want active a.wav hit", hits)
	}

	if err := os.WriteFile(path, []byte(fennelSourceSample("b.wav")), 0o644); err != nil {
		t.Fatalf("os.WriteFile(recreate) error = %v, want nil", err)
	}
	deadline := time.Now().Add(2 * time.Second)
	for bar := 2; time.Now().Before(deadline); bar++ {
		hits, _, _, err := provider.Next(bar)
		if err != nil {
			t.Fatalf("Next(%d) error = %v, want nil", bar, err)
		}
		if len(hits) == 1 && hits[0].Sample == "b.wav" {
			return
		}
		time.Sleep(20 * time.Millisecond)
	}
	t.Fatal("recreated file was not observed before deadline")
}
