package engine

import (
	"io"
	"math"
	"testing"

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
