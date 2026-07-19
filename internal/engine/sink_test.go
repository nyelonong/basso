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

// TestKarplusStrongStreamer_SampleCount verifies that streaming a
// karplusStrongStreamer to exhaustion (same countSamples technique
// TestSynthesizeNote_SampleCount uses) produces exactly numSamples samples,
// for a range of lengths including one shorter than the delay-line buffer
// itself.
func TestKarplusStrongStreamer_SampleCount(t *testing.T) {
	tests := []struct {
		name       string
		freq       float64
		numSamples int
	}{
		{name: "normal note", freq: 110.0, numSamples: deviceSampleRate.N(250 * time.Millisecond)},
		{name: "long note", freq: 220.0, numSamples: deviceSampleRate.N(2 * time.Second)},
		{name: "shorter than the delay line", freq: 110.0, numSamples: 5},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			s := newKarplusStrongStreamer(deviceSampleRate, tt.freq, tt.numSamples)

			got := countSamples(s)
			if got != tt.numSamples {
				t.Errorf("countSamples() = %d, want %d", got, tt.numSamples)
			}
		})
	}
}

// TestKarplusStrongStreamer_Decays is a real correctness check on the
// feedback loop, not just "did it run": it streams a long note and compares
// the average absolute sample value over the first ~10% of samples against
// the last ~10%. A correct implementation's tail is meaningfully quieter
// than its head (proven empirically: ratio ~0.25-0.30 for this freq/
// duration). A broken feedback loop — e.g. the decayed value never written
// back to the buffer, so the same noise loops unchanged — stays at a ratio
// of ~1.0, which this test's threshold (0.7) rejects.
func TestKarplusStrongStreamer_Decays(t *testing.T) {
	numSamples := deviceSampleRate.N(500 * time.Millisecond)
	s := newKarplusStrongStreamer(deviceSampleRate, 110.0, numSamples)

	buf := make([][2]float64, numSamples)
	n, ok := s.Stream(buf)
	if n != numSamples || !ok {
		t.Fatalf("Stream() = (%d, %v), want (%d, true)", n, ok, numSamples)
	}

	tenPct := numSamples / 10
	var headSum, tailSum float64
	for i := 0; i < tenPct; i++ {
		headSum += math.Abs(buf[i][0])
	}
	for i := numSamples - tenPct; i < numSamples; i++ {
		tailSum += math.Abs(buf[i][0])
	}
	headAvg := headSum / float64(tenPct)
	tailAvg := tailSum / float64(tenPct)

	if tailAvg >= headAvg*0.7 {
		t.Errorf("tailAvg = %.5f, headAvg = %.5f (ratio %.3f) — want tail meaningfully quieter than head (ratio < 0.7)", tailAvg, headAvg, tailAvg/headAvg)
	}
}
