package engine

import (
	"time"

	atomix "gopkg.in/mix.v0"
)

// atomixSink adapts gopkg.in/mix.v0's package-level API to AudioSink. It has
// no fields: gopkg.in/mix.v0 is itself a package-level singleton, so every
// atomixSink method just delegates to the corresponding atomix.X func.
type atomixSink struct{}

// NewAtomixSink returns the real AudioSink, backed by gopkg.in/mix.v0.
func NewAtomixSink() AudioSink {
	return atomixSink{}
}

// Start delegates to atomix.Start, opening the audio device.
func (atomixSink) Start() {
	atomix.Start()
}

// SetFire delegates to atomix.SetFire, discarding the returned *fire.Fire
// handle (AudioSink callers don't need it).
func (atomixSink) SetFire(source string, begin, sustain time.Duration, volume, pan float64) {
	atomix.SetFire(source, begin, sustain, volume, pan)
}

// Teardown delegates to atomix.Teardown, closing the audio device and
// releasing its resources.
func (atomixSink) Teardown() {
	atomix.Teardown()
}
