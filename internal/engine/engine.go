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

// Engine plays a pattern continuously, bar by bar, through an AudioSink.
type Engine struct {
	sink AudioSink
}

// NewEngine creates an Engine that schedules hits through sink.
func NewEngine(sink AudioSink) *Engine {
	return &Engine{sink: sink}
}

// Run opens the audio device once, then asks provider for each bar's hits,
// in order starting at bar 0, and schedules them through the AudioSink on a
// continuous clock: each bar's start is the previous bar's start plus that
// bar's duration. The device stays open across bars. Run returns when
// ctx is cancelled, or the error provider.Next produces once the pattern
// ends; either way it always tears the device down exactly once before
// returning.
func (e *Engine) Run(ctx context.Context, provider PatternProvider) error {
	e.sink.Start()
	defer e.sink.Teardown()

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
	}
}
