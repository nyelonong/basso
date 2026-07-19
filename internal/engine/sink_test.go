package engine

import (
	"io"
	"math"
	"testing"
	"time"

	"github.com/gopxl/beep/v2"
)

// realSoundsPath points sample-cache tests at the repo's real sound/808/
// WAV files (relative to this package's directory).
const realSoundsPath = "../../sound/808"

// TestSampleCache_CachesDecodedBuffer verifies that fetching the same
// sample name twice only decodes the underlying WAV file once: the second
// get returns the same cached *beep.Buffer without calling decode again.
func TestSampleCache_CachesDecodedBuffer(t *testing.T) {
	c := newSampleCache(realSoundsPath)

	decodeCalls := 0
	realDecode := c.decode
	c.decode = func(r io.Reader) (beep.StreamSeekCloser, beep.Format, error) {
		decodeCalls++
		return realDecode(r)
	}

	first, err := c.get("kick2.wav")
	if err != nil {
		t.Fatalf("get(kick2.wav) #1 error = %v, want nil", err)
	}

	second, err := c.get("kick2.wav")
	if err != nil {
		t.Fatalf("get(kick2.wav) #2 error = %v, want nil", err)
	}

	if decodeCalls != 1 {
		t.Errorf("decodeCalls = %d, want 1 (second get should hit cache)", decodeCalls)
	}

	if first != second {
		t.Errorf("get() returned different *beep.Buffer values across calls, want the same cached instance")
	}
}

// TestSampleCache_DecodesRealWAV verifies that a real sample from
// sound/808/ decodes successfully into a non-empty buffer.
func TestSampleCache_DecodesRealWAV(t *testing.T) {
	c := newSampleCache(realSoundsPath)

	buf, err := c.get("kick2.wav")
	if err != nil {
		t.Fatalf("get(kick2.wav) error = %v, want nil", err)
	}

	if buf.Len() == 0 {
		t.Errorf("buf.Len() = 0, want > 0")
	}
}

// TestVolumeParams verifies the velocity(0..1)->effects.Volume conversion:
// volume=1.0 maps to Volume≈0 (unchanged gain, since Base^0 == 1), and any
// volume<=0 maps to Silent: true (guarding math.Log2(0) == -Inf).
func TestVolumeParams(t *testing.T) {
	v, silent := volumeParams(1.0)
	if silent {
		t.Errorf("volumeParams(1.0) silent = true, want false")
	}
	if math.Abs(v) > 1e-9 {
		t.Errorf("volumeParams(1.0) volume = %v, want ≈0", v)
	}

	if _, silent := volumeParams(0); !silent {
		t.Errorf("volumeParams(0) silent = false, want true")
	}

	if _, silent := volumeParams(-0.5); !silent {
		t.Errorf("volumeParams(-0.5) silent = false, want true")
	}
}

// countSamples drains s by repeated Stream() calls (same technique
// TestSampleCache_DecodesRealWAV uses for real-file computation without a
// device) and returns the total number of samples it produced.
func countSamples(s beep.Streamer) int {
	total := 0
	buf := make([][2]float64, 512)
	for {
		n, ok := s.Stream(buf)
		total += n
		if !ok {
			return total
		}
	}
}

// TestSynthesizeNote_SampleCount verifies that the note-to-streamer path
// (SawtoothTone, cut to length, enveloped) produces exactly
// deviceSampleRate.N(sustain) samples, for a range of sustains including a
// very short one where the attack+release envelope must clamp to fit.
func TestSynthesizeNote_SampleCount(t *testing.T) {
	tests := []struct {
		name    string
		sustain time.Duration
	}{
		{name: "normal note", sustain: 250 * time.Millisecond},
		{name: "long note", sustain: 2 * time.Second},
		{name: "very short note", sustain: 5 * time.Millisecond},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			streamer, err := synthesizeNote(110.0, tt.sustain)
			if err != nil {
				t.Fatalf("synthesizeNote() error = %v, want nil", err)
			}

			want := deviceSampleRate.N(tt.sustain)
			got := countSamples(streamer)
			if got != want {
				t.Errorf("countSamples() = %d, want %d", got, want)
			}
		})
	}
}

// TestSynthesizeNote_RejectsAboveNyquist verifies that a frequency at or
// above the Nyquist limit for deviceSampleRate returns an error (propagated
// from generators.SawtoothTone) instead of a panic.
func TestSynthesizeNote_RejectsAboveNyquist(t *testing.T) {
	if _, err := synthesizeNote(float64(deviceSampleRate)/2, 100*time.Millisecond); err == nil {
		t.Fatalf("synthesizeNote() error = nil, want error for a frequency at Nyquist")
	}
}
