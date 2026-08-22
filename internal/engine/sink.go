package engine

import (
	"fmt"
	"io"
	"math"
	"math/rand"
	"os"
	"path/filepath"
	"sync"
	"time"

	"github.com/gopxl/beep/v2"
	"github.com/gopxl/beep/v2/effects"
	"github.com/gopxl/beep/v2/generators"
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

// bassAttack is the fixed attack ramp applied to every synthesized note, so
// it doesn't click in at full volume.
const bassAttack = 8 * time.Millisecond

// bassMaxRelease is the fixed cap on the release ramp's length; the actual
// release is also capped to bassReleaseFraction of the note's total length,
// whichever is smaller (see splitEnvelope).
const bassMaxRelease = 30 * time.Millisecond

// bassReleaseFraction is the release ramp's cap as a fraction of the note's
// total sample count, so a short note's release doesn't eat more than a
// reasonable slice of the note.
const bassReleaseFraction = 0.2

// splitEnvelope divides totalN samples into an attack ramp, a plain sustain
// portion, and a release ramp, clamped so attackN+releaseN never exceeds
// totalN (a very short note must not get overlapping/negative segments).
// The release is capped at both maxRelease and releaseFraction of totalN,
// whichever is smaller; the attack is capped to whatever's left after that.
// attack/maxRelease/releaseFraction are per-instrument (bass, brass, ...),
// letting each voice shape its own envelope through the same clamping logic.
func splitEnvelope(totalN int, attack, maxRelease time.Duration, releaseFraction float64) (attackN, releaseN int) {
	attackN = deviceSampleRate.N(attack)
	releaseN = deviceSampleRate.N(maxRelease)
	if capped := int(float64(totalN) * releaseFraction); releaseN > capped {
		releaseN = capped
	}
	if releaseN < 0 {
		releaseN = 0
	}
	if attackN > totalN-releaseN {
		attackN = totalN - releaseN
	}
	if attackN < 0 {
		attackN = 0
	}
	return attackN, releaseN
}

// brassAttack is brass's slower attack ramp than bass's punchy stab — a
// "swell," the way a brass player's air pressure builds.
const brassAttack = 40 * time.Millisecond

// brassMaxRelease is brass's longer release cap than bass's, capped the
// same way bassMaxRelease is (see splitEnvelope).
const brassMaxRelease = 100 * time.Millisecond

func applyEnvelope(tone beep.Streamer, sustain, attack, maxRelease time.Duration, releaseFraction float64) beep.Streamer {
	totalN := deviceSampleRate.N(sustain)
	attackN, releaseN := splitEnvelope(totalN, attack, maxRelease, releaseFraction)
	sustainN := totalN - attackN - releaseN

	var segments []beep.Streamer
	if attackN > 0 {
		segments = append(segments, effects.Transition(beep.Take(attackN, tone), attackN, 0, 1, effects.TransitionLinear))
	}
	if sustainN > 0 {
		segments = append(segments, beep.Take(sustainN, tone))
	}
	if releaseN > 0 {
		segments = append(segments, effects.Transition(beep.Take(releaseN, tone), releaseN, 1, 0, effects.TransitionLinear))
	}
	return beep.Seq(segments...)
}

// synthesizeSawtoothNote builds a finite streamer for a sawtooth-oscillator
// note at freq, sustaining for exactly deviceSampleRate.N(sustain) samples:
// generators.SawtoothTone (an infinite oscillator, hence the cut) enveloped
// with attack/release (effects.Transition, shaped by splitEnvelope) so it
// doesn't click at the start or end. synthesizeNote (bass) and
// synthesizeBrass are both thin wrappers over this, differing only in their
// attack/maxRelease/releaseFraction — the oscillator and envelope assembly
// are shared, not duplicated. No caching (unlike sampleCache for WAVs):
// synthesizing a tone is cheap pure computation, not disk I/O.
func synthesizeSawtoothNote(freq float64, sustain time.Duration, attack, maxRelease time.Duration, releaseFraction float64) (beep.Streamer, error) {
	tone, err := generators.SawtoothTone(deviceSampleRate, freq)
	if err != nil {
		return nil, fmt.Errorf("synthesizeSawtoothNote: %w", err)
	}
	return applyEnvelope(tone, sustain, attack, maxRelease, releaseFraction), nil
}

// synthesizeNote builds a synthesized bass note: same-shaped envelope as
// synthesizeBrass, but bass's punchier, quicker attack/release.
func synthesizeNote(freq float64, sustain time.Duration) (beep.Streamer, error) {
	return synthesizeSawtoothNote(freq, sustain, bassAttack, bassMaxRelease, bassReleaseFraction)
}

// synthesizeBrass builds a synthesized brass note: the same sawtooth
// oscillator as bass (its bright, buzzy harmonic content is what reads as
// "brass" without a filter), but a slower attack ("swell") and longer
// release than bass's punchy stab.
func synthesizeBrass(freq float64, sustain time.Duration) (beep.Streamer, error) {
	return synthesizeSawtoothNote(freq, sustain, brassAttack, brassMaxRelease, bassReleaseFraction)
}

// pluckMaxRelease and pluckReleaseFraction shape pluck's tail-only release
// fade (no attack ramp — see newKarplusStrongStreamer's doc comment), capped
// the same way splitEnvelope caps bass/brass releases.
const pluckMaxRelease = 12 * time.Millisecond

// karplusStrongStreamer synthesizes a plucked-string tone via Karplus-Strong:
// a circular delay-line buffer of length round(sampleRate/freq) samples,
// seeded with white noise. Each sample read from the buffer is immediately
// replaced by decay * the average of itself and its neighbor (a simple
// 2-tap lowpass + attenuation) before advancing — high harmonics decay
// faster than the fundamental, producing the characteristic plucked-string
// timbre: a bright noisy onset settling into a decaying tone at freq.
type karplusStrongStreamer struct {
	buf       []float64
	pos       int
	decay     float64
	remaining int
}

// newKarplusStrongStreamer returns a karplusStrongStreamer tuned to freq,
// producing exactly numSamples samples before exhausting. n is clamped to a
// minimum of 2 to guard against a degenerate buffer at very high
// frequencies.
func newKarplusStrongStreamer(sr beep.SampleRate, freq float64, numSamples int) beep.Streamer {
	n := int(math.Round(float64(sr) / freq))
	if n < 2 {
		n = 2 // guard against a degenerate buffer at very high frequencies
	}
	buf := make([]float64, n)
	for i := range buf {
		buf[i] = rand.Float64()*2 - 1
	}
	return &karplusStrongStreamer{buf: buf, decay: 0.996, remaining: numSamples}
}

func (k *karplusStrongStreamer) Stream(samples [][2]float64) (n int, ok bool) {
	for i := range samples {
		if k.remaining <= 0 {
			return i, i > 0
		}
		v := k.buf[k.pos]
		next := k.buf[(k.pos+1)%len(k.buf)]
		k.buf[k.pos] = k.decay * 0.5 * (v + next)
		samples[i][0] = v
		samples[i][1] = v
		k.pos = (k.pos + 1) % len(k.buf)
		k.remaining--
	}
	return len(samples), true
}

func (k *karplusStrongStreamer) Err() error { return nil }

// synthesizePluck builds a finite streamer for a plucked-string note at
// freq, sustaining for exactly deviceSampleRate.N(sustain) samples: Karplus-
// Strong synthesis (karplusStrongStreamer), enveloped with a short release
// fade only — no attack ramp, since the noisy onset karplusStrongStreamer
// already produces is the pluck's attack character; softening it would be
// wrong. Reuses splitEnvelope for the release-length clamping (attack
// passed as 0), the same effects.Transition/beep.Take/beep.Seq composition
// synthesizeNote and synthesizeBrass use.
func synthesizePluck(freq float64, sustain time.Duration) (beep.Streamer, error) {
	totalN := deviceSampleRate.N(sustain)
	tone := newKarplusStrongStreamer(deviceSampleRate, freq, totalN)

	_, releaseN := splitEnvelope(totalN, 0, pluckMaxRelease, bassReleaseFraction)
	sustainN := totalN - releaseN

	var segments []beep.Streamer
	if sustainN > 0 {
		segments = append(segments, beep.Take(sustainN, tone))
	}
	if releaseN > 0 {
		segments = append(segments, effects.Transition(beep.Take(releaseN, tone), releaseN, 1, 0, effects.TransitionLinear))
	}
	return beep.Seq(segments...), nil
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

// SetFireNote waits, in its own goroutine, until reference+begin is reached
// (or returns immediately if that's already passed), then synthesizes note
// as instrument's voice sustaining for sustain, wraps it in Volume then Pan
// — the same pattern SetFire uses for sample-based hits — and plays it.
// Unlike SetFire's sustain (currently unused, Engine.Run always passes 0),
// sustain here is load-bearing: it's how long the tone rings before its
// envelope closes it out. instrument selects the synthesis path; an unknown
// instrument (or a malformed note, same precedent) logs to stderr and
// silently skips the hit rather than erroring the whole engine.
func (s *beepSink) SetFireNote(note string, instrument string, begin, sustain time.Duration, volume, pan float64) {
	go func() {
		if wait := time.Until(s.reference.Add(begin)); wait > 0 {
			time.Sleep(wait)
		}

		freq, err := parseNote(note)
		if err != nil {
			fmt.Fprintln(os.Stderr, "basso: SetFireNote:", err)
			return
		}

		if instrument == "" {
			instrument = "bass"
		}
		preset, ok := findInstrument(instrument)
		if !ok {
			err = fmt.Errorf("beepSink: unknown instrument %q", instrument)
		}
		var tone beep.Streamer
		if err == nil {
			tone, err = preset.synthesize(freq, sustain)
		}
		if err != nil {
			fmt.Fprintln(os.Stderr, "basso: SetFireNote:", err)
			return
		}

		v, silent := volumeParams(volume)
		streamer := &effects.Pan{
			Pan: pan,
			Streamer: &effects.Volume{
				Streamer: tone,
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
