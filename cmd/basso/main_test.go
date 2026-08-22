package main

import (
	"bytes"
	"context"
	"errors"
	"fmt"
	"io"
	"reflect"
	"strings"
	"testing"
	"time"

	"github.com/nyelonong/basso/internal/engine"
)

// fakeSink is a no-op engine.AudioSink stub: it never touches a real audio
// device, so run() can be exercised in tests without SDL2/PortAudio/an
// actual device present.
type fakeSink struct{}

func (fakeSink) Start()                                                                   {}
func (fakeSink) Teardown()                                                                {}
func (fakeSink) SetFire(source string, begin, sustain time.Duration, volume, pan float64) {}
func (fakeSink) SetFireNote(note string, instrument string, begin, sustain time.Duration, volume, pan float64) {
}

func newFakeSink() engine.AudioSink { return fakeSink{} }

// stubProvider is a closablePatternProvider stub whose Next always returns
// err, so Engine.Run stops after its very first call instead of pacing
// against real bar durations.
type stubProvider struct {
	err error
}

func (s *stubProvider) Next(bar int) ([]engine.Hit, int, int, error) {
	return nil, 120, 16, s.err
}

func (s *stubProvider) Close() error { return nil }

// TestMain_ArgParsing verifies that both accepted argument forms resolve to
// the same file path, which is what gets passed to the provider constructor
// — not to any real file or real playback.
func TestMain_ArgParsing(t *testing.T) {
	tests := []struct {
		name string
		args []string
	}{
		{name: "play form", args: []string{"play", "foo.fnl"}},
		{name: "alias form", args: []string{"foo.fnl"}},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			var gotPath string
			stopErr := errors.New("stop after first bar")
			newProvider := func(path string, _ engine.DiagnosticReporter) (closablePatternProvider, error) {
				gotPath = path
				return &stubProvider{err: stopErr}, nil
			}

			err := run(context.Background(), tt.args, playbackObservers{}, newProvider, newFakeSink)

			if gotPath != "foo.fnl" {
				t.Errorf("resolved path = %q, want %q", gotPath, "foo.fnl")
			}
			if !errors.Is(err, stopErr) {
				t.Errorf("run() error = %v, want %v", err, stopErr)
			}
		})
	}
}

// TestMain_RejectsMissingArg verifies that invalid argument shapes return an
// error without ever calling the provider constructor (no panic either).
func TestMain_RejectsMissingArg(t *testing.T) {
	tests := []struct {
		name string
		args []string
	}{
		{name: "no args", args: []string{}},
		{name: "play with no file", args: []string{"play"}},
		{name: "play with extra args", args: []string{"play", "a.fnl", "b.fnl"}},
		{name: "two bare args", args: []string{"a.fnl", "b.fnl"}},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			called := false
			newProvider := func(path string, _ engine.DiagnosticReporter) (closablePatternProvider, error) {
				called = true
				return nil, nil
			}

			err := run(context.Background(), tt.args, playbackObservers{}, newProvider, newFakeSink)

			if err == nil {
				t.Fatalf("run() error = nil, want error")
			}
			if called {
				t.Errorf("provider constructor was called, want not called")
			}
		})
	}
}

func TestStderrDiagnosticReporter(t *testing.T) {
	var stderr bytes.Buffer
	reporter := stderrDiagnosticReporter(&stderr)
	bar := 3
	reporter(engine.Diagnostic{
		RevisionSHA256: strings.Repeat("a", 64),
		Bar:            &bar,
		Phase:          engine.DiagnosticPhaseValidate,
		Err:            errors.New("bad hit"),
	})

	got := stderr.String()
	for _, want := range []string{
		strings.Repeat("a", 64),
		"bar 3",
		"validate",
		"bad hit",
	} {
		if !strings.Contains(got, want) {
			t.Errorf("stderr = %q, want substring %q", got, want)
		}
	}
}

func TestRun_InvalidInitialSourceDoesNotConstructSink(t *testing.T) {
	invalid := errors.New("invalid initial source")
	sinkConstructed := false

	err := run(
		context.Background(),
		[]string{"pattern.fnl"},
		playbackObservers{},
		func(string, engine.DiagnosticReporter) (closablePatternProvider, error) {
			return nil, invalid
		},
		func() engine.AudioSink {
			sinkConstructed = true
			return newFakeSink()
		},
	)

	if !errors.Is(err, invalid) {
		t.Fatalf("run() error = %v, want %v", err, invalid)
	}
	if sinkConstructed {
		t.Fatal("audio sink was constructed for invalid initial source")
	}
}

func TestDispatch_PreservesPlayAndBareAlias(t *testing.T) {
	tests := []struct {
		name string
		args []string
	}{
		{name: "play", args: []string{"play", "pattern.fnl"}},
		{name: "bare alias", args: []string{"pattern.fnl"}},
	}

	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			stopErr := errors.New("stop playback")
			var gotPath string
			deps := commandDependencies{
				stdout: io.Discard,
				stderr: io.Discard,
				newProvider: func(path string, _ engine.DiagnosticReporter) (closablePatternProvider, error) {
					gotPath = path
					return &stubProvider{err: stopErr}, nil
				},
				newSink: newFakeSink,
			}

			err := runCommand(context.Background(), test.args, deps)

			if !errors.Is(err, stopErr) {
				t.Fatalf("runCommand() error = %v, want %v", err, stopErr)
			}
			if gotPath != "pattern.fnl" {
				t.Errorf("provider path = %q, want pattern.fnl", gotPath)
			}
		})
	}
}

// countingProvider is a closablePatternProvider stub whose Next succeeds
// with fixed tempo values for `stops` bars, then always returns stopErr, so
// Engine.Run paces through real bars before stopping deterministically.
type countingProvider struct {
	stops   int
	stopErr error
}

func (s *countingProvider) Next(bar int) ([]engine.Hit, int, int, error) {
	if bar >= s.stops {
		return nil, 0, 0, s.stopErr
	}
	return nil, 400, 16, nil
}

func (s *countingProvider) Close() error { return nil }

// TestRun_ObservesBarsAndDiagnostics proves the observer seam: every
// successful bar reaches onBar with its bpm and steps per bar, and the
// diagnostic reporter handed to the provider constructor forwards into
// onDiagnostic.
func TestRun_ObservesBarsAndDiagnostics(t *testing.T) {
	stopErr := errors.New("stop after second bar")
	reported := make(chan engine.Diagnostic, 1)

	newProvider := func(path string, onDiagnostic engine.DiagnosticReporter) (closablePatternProvider, error) {
		if onDiagnostic == nil {
			t.Error("provider constructor received nil diagnostic reporter")
		}
		bar := 2
		onDiagnostic(engine.Diagnostic{
			RevisionSHA256: strings.Repeat("b", 64),
			Bar:            &bar,
			Phase:          engine.DiagnosticPhaseValidate,
			Err:            errors.New("stale revision"),
		})
		return &countingProvider{stops: 2, stopErr: stopErr}, nil
	}

	var bars []string
	observers := playbackObservers{
		onBar: func(bar, bpm, stepsPerBar int, _ []engine.Hit) {
			bars = append(bars, fmt.Sprintf("%d/%d/%d", bar, bpm, stepsPerBar))
		},
		onDiagnostic: func(d engine.Diagnostic) { reported <- d },
	}
	err := run(context.Background(), []string{"pattern.fnl"}, observers, newProvider, newFakeSink)
	if !errors.Is(err, stopErr) {
		t.Fatalf("run() error = %v, want %v", err, stopErr)
	}

	wantBars := []string{"0/400/16", "1/400/16"}
	if !reflect.DeepEqual(bars, wantBars) {
		t.Errorf("onBar events = %v, want %v", bars, wantBars)
	}

	select {
	case d := <-reported:
		if d.Phase != engine.DiagnosticPhaseValidate || d.Bar == nil || *d.Bar != 2 {
			t.Errorf("diagnostic = %+v, want validate phase at bar 2", d)
		}
	default:
		t.Error("onDiagnostic was never called")
	}
}
