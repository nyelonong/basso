package engine

import (
	"math"
	"reflect"
	"testing"
	"time"

	"github.com/gopxl/beep/v2"
	"github.com/gopxl/beep/v2/generators"
)

type fixedValueStreamer struct {
	value     [2]float64
	remaining int
}

func (s *fixedValueStreamer) Stream(samples [][2]float64) (int, bool) {
	n := min(len(samples), s.remaining)
	for i := range n {
		samples[i] = s.value
	}
	s.remaining -= n
	return n, s.remaining > 0
}

func (*fixedValueStreamer) Err() error { return nil }

func collectSamples(streamer beep.Streamer) [][2]float64 {
	var samples [][2]float64
	buffer := make([][2]float64, 64)
	for {
		n, ok := streamer.Stream(buffer)
		samples = append(samples, buffer[:n]...)
		if !ok {
			return samples
		}
	}
}

func TestWeightedOscillatorMixer(t *testing.T) {
	weights := []float64{0.6, 0.4}
	streamer, err := newWeightedMixer([]beep.Streamer{
		&fixedValueStreamer{value: [2]float64{1, 1}, remaining: 5},
		&fixedValueStreamer{value: [2]float64{-0.5, -0.5}, remaining: 3},
	}, weights)
	if err != nil {
		t.Fatalf("newWeightedMixer() error = %v", err)
	}
	weights[0] = 0

	samples := collectSamples(streamer)
	if len(samples) != 5 {
		t.Fatalf("sample count = %d, want 5", len(samples))
	}
	for i, sample := range samples {
		want := 0.4
		if i >= 3 {
			want = 0.6
		}
		if math.Abs(sample[0]-want) > 1e-12 || math.Abs(sample[1]-want) > 1e-12 {
			t.Errorf("sample[%d] = %v, want [%v %v]", i, sample, want, want)
		}
	}
}

func TestWeightedOscillatorMixerRejectsInvalidInput(t *testing.T) {
	tests := []struct {
		name      string
		streamers []beep.Streamer
		weights   []float64
	}{
		{name: "empty"},
		{name: "length mismatch", streamers: []beep.Streamer{&fixedValueStreamer{}}, weights: []float64{0.5, 0.5}},
		{name: "nil streamer", streamers: []beep.Streamer{nil}, weights: []float64{1}},
		{name: "negative weight", streamers: []beep.Streamer{&fixedValueStreamer{}}, weights: []float64{-0.1}},
		{name: "non-finite weight", streamers: []beep.Streamer{&fixedValueStreamer{}}, weights: []float64{math.NaN()}},
		{name: "zero total", streamers: []beep.Streamer{&fixedValueStreamer{}}, weights: []float64{0}},
		{name: "total above one", streamers: []beep.Streamer{&fixedValueStreamer{}, &fixedValueStreamer{}}, weights: []float64{0.6, 0.5}},
	}
	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			if _, err := newWeightedMixer(test.streamers, test.weights); err == nil {
				t.Fatal("newWeightedMixer() error = nil, want error")
			}
		})
	}
}

func TestOnePoleLowPass(t *testing.T) {
	const sampleCount = 123
	source, err := generators.SineTone(deviceSampleRate, 440)
	if err != nil {
		t.Fatal(err)
	}
	streamer, err := newOnePoleLowPass(beep.Take(sampleCount, source), deviceSampleRate, 1_000)
	if err != nil {
		t.Fatalf("newOnePoleLowPass() error = %v", err)
	}
	first := collectSamples(streamer)
	if len(first) != sampleCount {
		t.Fatalf("sample count = %d, want %d", len(first), sampleCount)
	}
	for i, sample := range first {
		if math.IsNaN(sample[0]) || math.IsInf(sample[0], 0) || math.IsNaN(sample[1]) || math.IsInf(sample[1], 0) {
			t.Fatalf("sample[%d] is not finite: %v", i, sample)
		}
	}

	source, _ = generators.SineTone(deviceSampleRate, 440)
	repeated, _ := newOnePoleLowPass(beep.Take(sampleCount, source), deviceSampleRate, 1_000)
	if second := collectSamples(repeated); !reflect.DeepEqual(first, second) {
		t.Fatal("repeated low-pass construction produced different samples")
	}
}

func TestOnePoleLowPassAttenuatesAboveCutoff(t *testing.T) {
	gain := func(t *testing.T, frequency float64) float64 {
		t.Helper()
		const sampleCount = 44_100
		source, err := generators.SineTone(deviceSampleRate, frequency)
		if err != nil {
			t.Fatal(err)
		}
		filtered, err := newOnePoleLowPass(beep.Take(sampleCount, source), deviceSampleRate, 1_000)
		if err != nil {
			t.Fatal(err)
		}
		samples := collectSamples(filtered)
		var sum float64
		for _, sample := range samples[sampleCount/10:] {
			sum += sample[0] * sample[0]
		}
		return math.Sqrt(sum / float64(len(samples)-sampleCount/10))
	}

	low := gain(t, 100)
	high := gain(t, 10_000)
	if high >= low*0.25 {
		t.Fatalf("high-frequency RMS = %.4f, low-frequency RMS = %.4f; want high < 25%% of low", high, low)
	}
}

func TestOnePoleLowPassRejectsInvalidInput(t *testing.T) {
	source := &fixedValueStreamer{}
	for _, cutoff := range []float64{0, -1, math.NaN(), math.Inf(1), float64(deviceSampleRate) / 2} {
		if _, err := newOnePoleLowPass(source, deviceSampleRate, cutoff); err == nil {
			t.Errorf("newOnePoleLowPass(cutoff=%v) error = nil, want error", cutoff)
		}
	}
	if _, err := newOnePoleLowPass(nil, deviceSampleRate, 1_000); err == nil {
		t.Error("newOnePoleLowPass(nil) error = nil, want error")
	}
}

func TestSynthEnvelopeClampsShortDurations(t *testing.T) {
	for _, total := range []int{0, 1, 10, deviceSampleRate.N(5 * time.Millisecond)} {
		attack, release := splitEnvelope(total, 40*time.Millisecond, 100*time.Millisecond, 0.2)
		if attack < 0 || release < 0 || attack+release > total {
			t.Errorf("splitEnvelope(%d) = (%d, %d), want non-negative sum <= total", total, attack, release)
		}
	}
}
