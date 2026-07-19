package engine

import (
	"fmt"
	"io"
	"math"
	"os"
	"path/filepath"
	"sync"
	"time"

	"github.com/gopxl/beep/v2"
	"github.com/gopxl/beep/v2/effects"
	"github.com/gopxl/beep/v2/speaker"
	"github.com/gopxl/beep/v2/wav"
)

// deviceSampleRate is the format every sample is decoded and played at.
// Every sound/808/ WAV file is confirmed mono/44100Hz/16-bit (via soxi), so
// decoding at this rate never needs resampling.
const deviceSampleRate = beep.SampleRate(44100)

// sampleCache lazily decodes WAV files from soundsPath into *beep.Buffer
// values and caches them by sample name, so repeated fires of the same
// sample never re-decode from disk. decode defaults to wav.Decode; tests
// override it to observe/count decode calls. Guarded by mu, since
// concurrent SetFire goroutines (see beepSink) can race to load samples.
type sampleCache struct {
	soundsPath string
	decode     func(r io.Reader) (beep.StreamSeekCloser, beep.Format, error)

	mu   sync.Mutex
	bufs map[string]*beep.Buffer
}

func newSampleCache(soundsPath string) *sampleCache {
	return &sampleCache{
		soundsPath: soundsPath,
		decode:     wav.Decode,
		bufs:       make(map[string]*beep.Buffer),
	}
}

// get returns the cached *beep.Buffer for name, decoding and caching it
// from <soundsPath>/<name> first if this is the first request for name.
func (c *sampleCache) get(name string) (*beep.Buffer, error) {
	c.mu.Lock()
	defer c.mu.Unlock()

	if buf, ok := c.bufs[name]; ok {
		return buf, nil
	}

	f, err := os.Open(filepath.Join(c.soundsPath, name))
	if err != nil {
		return nil, fmt.Errorf("sampleCache: open %s: %w", name, err)
	}
	defer f.Close()

	streamer, format, err := c.decode(f)
	if err != nil {
		return nil, fmt.Errorf("sampleCache: decode %s: %w", name, err)
	}
	defer streamer.Close()

	buf := beep.NewBuffer(format)
	buf.Append(streamer)

	c.bufs[name] = buf
	return buf, nil
}

// volumeParams converts a linear volume (0..1, from Hit.Velocity) into the
// exponential Base-2 parameters effects.Volume expects. Base 2, Volume
// math.Log2(volume) gives Base^Volume == volume exactly, since 2^log2(x) ==
// x. Silent guards volume<=0, where math.Log2 would return -Inf.
func volumeParams(volume float64) (v float64, silent bool) {
	if volume <= 0 {
		return 0, true
	}
	return math.Log2(volume), false
}

// beepSink is the real AudioSink, backed by github.com/gopxl/beep/v2.
// SetFire has no "play later" primitive to call into (speaker.Play starts
// essentially immediately), so it spawns a goroutine per fire that waits
// until its scheduled time before playing.
type beepSink struct {
	cache     *sampleCache
	reference time.Time
}

// NewBeepSink returns the real AudioSink, backed by github.com/gopxl/beep/v2.
// soundsPath is the directory samples are loaded from, e.g. "sound/808/".
func NewBeepSink(soundsPath string) AudioSink {
	return &beepSink{cache: newSampleCache(soundsPath)}
}

// Start opens the audio device at deviceSampleRate with a ~33ms buffer,
// then captures the sink's reference time with a 1-second lead-in —
// matching the pre-rebuild atomixSink's lead-in — so SetFire goroutines and
// the device itself have time to spin up before the first hit is due.
func (s *beepSink) Start() {
	if err := speaker.Init(deviceSampleRate, deviceSampleRate.N(time.Second/30)); err != nil {
		fmt.Fprintln(os.Stderr, "basso: speaker.Init:", err)
	}
	s.reference = time.Now().Add(1 * time.Second)
}

// SetFire waits, in its own goroutine, until reference+begin is reached (or
// returns immediately if that's already passed), then loads source from
// the sample cache, wraps it in Volume then Pan, and plays it. sustain is
// part of the AudioSink interface but unused: Engine.Run always passes 0.
func (s *beepSink) SetFire(source string, begin, sustain time.Duration, volume, pan float64) {
	go func() {
		if wait := time.Until(s.reference.Add(begin)); wait > 0 {
			time.Sleep(wait)
		}

		buf, err := s.cache.get(source)
		if err != nil {
			fmt.Fprintln(os.Stderr, "basso: SetFire:", err)
			return
		}

		v, silent := volumeParams(volume)
		streamer := &effects.Pan{
			Pan: pan,
			Streamer: &effects.Volume{
				Streamer: buf.Streamer(0, buf.Len()),
				Base:     2,
				Volume:   v,
				Silent:   silent,
			},
		}
		speaker.Play(streamer)
	}()
}

// Teardown closes the audio device and releases its resources.
func (s *beepSink) Teardown() {
	speaker.Close()
}
