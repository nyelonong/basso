package engine

import (
	"errors"
	"math"
	"slices"

	"github.com/gopxl/beep/v2"
)

type weightedMixer struct {
	streamers []beep.Streamer
	weights   []float64
	done      []bool
	scratch   [][2]float64
}

func newWeightedMixer(streamers []beep.Streamer, weights []float64) (beep.Streamer, error) {
	if len(streamers) == 0 {
		return nil, errors.New("weighted mixer requires at least one streamer")
	}
	if len(streamers) != len(weights) {
		return nil, errors.New("weighted mixer streamers and weights must have equal lengths")
	}

	var total float64
	for i, streamer := range streamers {
		if streamer == nil {
			return nil, errors.New("weighted mixer streamer must not be nil")
		}
		weight := weights[i]
		if math.IsNaN(weight) || math.IsInf(weight, 0) || weight < 0 {
			return nil, errors.New("weighted mixer weights must be finite and non-negative")
		}
		total += weight
	}
	if total <= 0 || total > 1+1e-12 {
		return nil, errors.New("weighted mixer weights must total within (0,1]")
	}

	return &weightedMixer{
		streamers: slices.Clone(streamers),
		weights:   slices.Clone(weights),
		done:      make([]bool, len(streamers)),
	}, nil
}

func (m *weightedMixer) Stream(samples [][2]float64) (int, bool) {
	clear(samples)
	if len(m.scratch) < len(samples) {
		m.scratch = make([][2]float64, len(samples))
	}

	maxN := 0
	more := false
	for i, streamer := range m.streamers {
		if m.done[i] {
			continue
		}
		scratch := m.scratch[:len(samples)]
		clear(scratch)
		n, ok := streamer.Stream(scratch)
		for j := range n {
			samples[j][0] += scratch[j][0] * m.weights[i]
			samples[j][1] += scratch[j][1] * m.weights[i]
		}
		if n > maxN {
			maxN = n
		}
		if ok {
			more = true
		} else {
			m.done[i] = true
		}
	}
	return maxN, more
}

func (m *weightedMixer) Err() error {
	for _, streamer := range m.streamers {
		if err := streamer.Err(); err != nil {
			return err
		}
	}
	return nil
}

type onePoleLowPass struct {
	streamer beep.Streamer
	alpha    float64
	previous [2]float64
}

func newOnePoleLowPass(streamer beep.Streamer, sampleRate beep.SampleRate, cutoff float64) (beep.Streamer, error) {
	if streamer == nil {
		return nil, errors.New("one-pole low-pass streamer must not be nil")
	}
	if sampleRate <= 0 {
		return nil, errors.New("one-pole low-pass sample rate must be positive")
	}
	nyquist := float64(sampleRate) / 2
	if math.IsNaN(cutoff) || math.IsInf(cutoff, 0) || cutoff <= 0 || cutoff >= nyquist {
		return nil, errors.New("one-pole low-pass cutoff must be finite and below nyquist")
	}
	alpha := 1 - math.Exp(-2*math.Pi*cutoff/float64(sampleRate))
	return &onePoleLowPass{streamer: streamer, alpha: alpha}, nil
}

func (f *onePoleLowPass) Stream(samples [][2]float64) (int, bool) {
	n, ok := f.streamer.Stream(samples)
	for i := range n {
		f.previous[0] += f.alpha * (samples[i][0] - f.previous[0])
		f.previous[1] += f.alpha * (samples[i][1] - f.previous[1])
		samples[i] = f.previous
	}
	return n, ok
}

func (f *onePoleLowPass) Err() error { return f.streamer.Err() }
