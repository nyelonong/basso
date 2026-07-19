package engine

import (
	"time"

	atomix "gopkg.in/mix.v0"
	"gopkg.in/mix.v0/bind/spec"
)

// atomixSink adapts gopkg.in/mix.v0's package-level API to AudioSink. It has
// no fields: gopkg.in/mix.v0 is itself a package-level singleton, so every
// atomixSink method just delegates to the corresponding atomix.X func.
type atomixSink struct{}

// NewAtomixSink returns the real AudioSink, backed by gopkg.in/mix.v0.
func NewAtomixSink() AudioSink {
	return atomixSink{}
}

// Start configures the mixer (48kHz, 32-bit float, stereo), points it at
// the sound/808/ WAV samples, and opens the audio device with a 1-second
// lead-in before the first fire, matching the pre-rebuild const.go config.
func (atomixSink) Start() {
	atomix.Configure(spec.AudioSpec{Freq: 48000, Format: spec.AudioF32, Channels: 2})
	atomix.SetSoundsPath("sound/808/")
	atomix.StartAt(time.Now().Add(1 * time.Second))
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
