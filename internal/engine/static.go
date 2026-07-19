package engine

import (
	"math/rand"
)

// StaticProvider is a PatternProvider that reproduces the pre-rebuild m001
// pattern exactly: 16 samples at bpm 120 with 16 steps per bar. This is a
// pure data fixture for testing and regression.
type StaticProvider struct{}

// Next returns the m001 pattern for any bar (the pattern does not vary by bar).
// It returns exactly 16 hits with the correct Step, Sample, and Velocity values,
// plus a randomized Pan value in the range [-1, 1] for each hit, matching the
// per-fire random pan behavior of the original code.
func (sp *StaticProvider) Next(bar int) ([]Hit, int, int, error) {
	hits := []Hit{
		{Step: 0, Sample: "kick2.wav", Velocity: 1.0, Pan: rand.Float64()*2 - 1},
		{Step: 1, Sample: "maracas.wav", Velocity: 1.0, Pan: rand.Float64()*2 - 1},
		{Step: 2, Sample: "cl_hihat.wav", Velocity: 1.0, Pan: rand.Float64()*2 - 1},
		{Step: 3, Sample: "maracas.wav", Velocity: 1.0, Pan: rand.Float64()*2 - 1},
		{Step: 4, Sample: "snare.wav", Velocity: 1.0, Pan: rand.Float64()*2 - 1},
		{Step: 5, Sample: "maracas.wav", Velocity: 1.0, Pan: rand.Float64()*2 - 1},
		{Step: 6, Sample: "cl_hihat.wav", Velocity: 1.0, Pan: rand.Float64()*2 - 1},
		{Step: 7, Sample: "kick2.wav", Velocity: 1.0, Pan: rand.Float64()*2 - 1},
		{Step: 8, Sample: "maracas.wav", Velocity: 1.0, Pan: rand.Float64()*2 - 1},
		{Step: 9, Sample: "maracas.wav", Velocity: 1.0, Pan: rand.Float64()*2 - 1},
		{Step: 10, Sample: "hightom.wav", Velocity: 1.0, Pan: rand.Float64()*2 - 1},
		{Step: 11, Sample: "maracas.wav", Velocity: 1.0, Pan: rand.Float64()*2 - 1},
		{Step: 12, Sample: "snare.wav", Velocity: 1.0, Pan: rand.Float64()*2 - 1},
		{Step: 13, Sample: "kick1.wav", Velocity: 1.0, Pan: rand.Float64()*2 - 1},
		{Step: 14, Sample: "cl_hihat.wav", Velocity: 1.0, Pan: rand.Float64()*2 - 1},
		{Step: 15, Sample: "maracas.wav", Velocity: 1.0, Pan: rand.Float64()*2 - 1},
	}
	return hits, 120, 16, nil
}
