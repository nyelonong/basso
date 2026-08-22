// Package engine plays a Fennel-scripted pattern continuously, bar by bar,
// through an AudioSink, and hot-reloads the pattern at bar boundaries.
package engine

import (
	"context"
	"time"
)

// Hit is a single sample or note trigger within a bar. It is sample-based
// (Sample set, Note empty) or note-based (Note set, Sample empty) —
// mutually exclusive. Ownership of defaulting/validation between the two
// lives in whoever constructs the Hit (e.g. FennelProvider); Engine just
// trusts the Hit it's given and dispatches on whether Note is empty.
type Hit struct {
	Step       int
	Sample     string // WAV filename; empty if Note is set
	Note       string // scientific pitch notation, e.g. "C2", "A#1"; empty if Sample is set
	Instrument string // "bass" (default), "brass", "pluck" — meaningful only when Note is set
	Length     int    // sustain, in steps — meaningful for Note hits only; ignored for Sample hits
	Pan        float64
	Velocity   float64
}

// PatternProvider supplies the hits, tempo (bpm), and bar length
// (stepsPerBar) in effect for a given bar. bar starts at 0 and increases by
// one every call.
type PatternProvider interface {
	Next(bar int) (hits []Hit, bpm int, stepsPerBar int, err error)
}

// AudioSink is the audio device seam. The real implementation (beepSink,
// in sink.go) wraps github.com/gopxl/beep/v2; tests use a fake.
type AudioSink interface {
	// Start opens the audio device and begins the playback clock.
	Start()

	// SetFire schedules source to play at begin (an offset from the sink's
	// playback start reference), sustaining for sustain, at the given
	// volume and pan.
	SetFire(source string, begin, sustain time.Duration, volume, pan float64)

	// SetFireNote schedules a synthesized tone at note (scientific pitch
	// notation, e.g. "C2"), voiced as instrument ("bass", "brass", "pluck"),
	// to play at begin, sustaining for sustain, at the given volume and pan.
	// Parallel to SetFire, but for note-based hits; unlike SetFire's sustain
	// (currently unused by Engine.Run), sustain here is load-bearing: how
	// long the synthesized tone rings before its envelope closes it out.
	SetFireNote(note string, instrument string, begin, sustain time.Duration, volume, pan float64)

	// Teardown closes the audio device and releases its resources.
	Teardown()
}

// clock is Engine's pacing seam: Run uses it to wait for each bar's real
// duration to pass before requesting the next one. realClock (below) backs
// the default NewEngine(sink) path; tests inject a fake so pacing can be
// proven deterministically, without real sleeps.
type clock interface {
	// Now returns the current time.
	Now() time.Time
	// WaitUntil blocks until t is reached or ctx is cancelled, whichever
	// comes first, returning ctx.Err() in the latter case.
	WaitUntil(ctx context.Context, t time.Time) error
}

// realClock paces Run in actual wall-clock time.
type realClock struct{}

func (realClock) Now() time.Time { return time.Now() }

func (realClock) WaitUntil(ctx context.Context, t time.Time) error {
	d := time.Until(t)
	if d <= 0 {
		select {
		case <-ctx.Done():
			return ctx.Err()
		default:
			return nil
		}
	}
	timer := time.NewTimer(d)
	defer timer.Stop()
	select {
	case <-ctx.Done():
		return ctx.Err()
	case <-timer.C:
		return nil
	}
}

// Engine plays a pattern continuously, bar by bar, through an AudioSink.
type Engine struct {
	sink  AudioSink
	clock clock
}

// NewEngine creates an Engine that schedules hits through sink, paced by the
// real wall clock.
func NewEngine(sink AudioSink) *Engine {
	return &Engine{sink: sink, clock: realClock{}}
}

// Run opens the audio device once, then asks provider for each bar's hits,
// in order starting at bar 0, and schedules them through the AudioSink on a
// continuous clock: each bar's start is the previous bar's start plus that
// bar's duration. After scheduling a bar's hits, Run paces itself against
// real bar boundaries: it waits, relative to a reference time captured once
// at entry, until that bar's duration has actually elapsed before asking
// provider for the next bar. The device stays open across bars. Run returns
// when ctx is cancelled — including mid-wait, promptly, rather than waiting
// out the rest of the bar — or the error provider.Next produces once the
// pattern ends; either way it always tears the device down exactly once
// before returning.
func (e *Engine) Run(ctx context.Context, provider PatternProvider) error {
	e.sink.Start()
	defer e.sink.Teardown()
	return e.Stream(ctx, provider)
}
