// Package engine plays a Fennel-scripted pattern continuously, bar by bar,
// through an AudioSink, and hot-reloads the pattern at bar boundaries.
package engine

import (
	"context"
	"time"
)

// Hit is a single sample trigger within a bar.
type Hit struct {
	Step     int
	Sample   string
	Pan      float64
	Velocity float64
}

// PatternProvider supplies the hits, tempo (bpm), and bar length
// (stepsPerBar) in effect for a given bar. bar starts at 0 and increases by
// one every call.
type PatternProvider interface {
	Next(bar int) (hits []Hit, bpm int, stepsPerBar int, err error)
}

// AudioSink is the audio device seam. The real implementation (atomixSink,
// in sink.go) wraps gopkg.in/mix.v0; tests use a fake.
type AudioSink interface {
	// Start opens the audio device and begins the playback clock.
	Start()

	// SetFire schedules source to play at begin (an offset from the sink's
	// playback start reference), sustaining for sustain, at the given
	// volume and pan.
	SetFire(source string, begin, sustain time.Duration, volume, pan float64)

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

	reference := e.clock.Now()
	var barStart time.Duration
	for bar := 0; ; bar++ {
		select {
		case <-ctx.Done():
			return ctx.Err()
		default:
		}

		hits, bpm, stepsPerBar, err := provider.Next(bar)
		if err != nil {
			return err
		}

		stepDuration := time.Minute / time.Duration(bpm*4)
		for _, h := range hits {
			e.sink.SetFire(h.Sample, barStart+time.Duration(h.Step)*stepDuration, 0, h.Velocity, h.Pan)
		}

		barStart += time.Duration(stepsPerBar) * stepDuration

		if err := e.clock.WaitUntil(ctx, reference.Add(barStart)); err != nil {
			return err
		}
	}
}
