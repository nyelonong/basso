package main

import (
	"context"
	"errors"
	"io"
	"testing"
	"time"

	"github.com/nyelonong/basso/internal/engine"
)

// fakeSink is a no-op engine.AudioSink stub: it never touches a real audio
// device, so run() can be exercised in tests without SDL2/PortAudio/an
// actual device present.
type fakeSink struct{}

func (fakeSink) Start()                                                                     {}
func (fakeSink) Teardown()                                                                  {}
func (fakeSink) SetFire(source string, begin, sustain time.Duration, volume, pan float64)   {}
func (fakeSink) SetFireNote(note string, begin, sustain time.Duration, volume, pan float64) {}

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
			newProvider := func(path string) (closablePatternProvider, error) {
				gotPath = path
				return &stubProvider{err: stopErr}, nil
			}

			err := run(context.Background(), tt.args, io.Discard, newProvider, newFakeSink)

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
			newProvider := func(path string) (closablePatternProvider, error) {
				called = true
				return nil, nil
			}

			err := run(context.Background(), tt.args, io.Discard, newProvider, newFakeSink)

			if err == nil {
				t.Fatalf("run() error = nil, want error")
			}
			if called {
				t.Errorf("provider constructor was called, want not called")
			}
		})
	}
}
