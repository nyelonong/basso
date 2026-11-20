// Command gen synthesizes the non-808 samples in sound/808 (mono, 44.1 kHz,
// 16-bit PCM, the format beepSink decodes). Run from the repository root:
//
//	go run ./sound/gen
//
// The sounds are built from oscillators, noise and filters only, so they carry
// no third-party sample license.
package main

import (
	"bytes"
	"encoding/binary"
	"fmt"
	"math"
	"math/rand/v2"
	"os"
	"path/filepath"
)

const rate = 44100

func main() {
	dir := "sound/808"
	if len(os.Args) > 1 {
		dir = os.Args[1]
	}
	for _, s := range kit() {
		path := filepath.Join(dir, s.name)
		if err := os.WriteFile(path, encodeWAV(s.samples), 0o644); err != nil {
			fmt.Fprintln(os.Stderr, err)
			os.Exit(1)
		}
		fmt.Printf("%s %.2fs\n", path, float64(len(s.samples))/rate)
	}
}

type sound struct {
	name    string
	samples []float64
}

func kit() []sound {
	rng := rand.New(rand.NewPCG(1, 2))
	return []sound{
		{"kick_hard.wav", kickHard(rng)},
		{"snare_hard.wav", snareHard(rng)},
		{"kendang_dung.wav", kendang(rng, 95, 40, -1, 0.22)},
		{"kendang_tung.wav", kendang(rng, 190, 0, 0.08, 0.15)},
		{"kendang_tak.wav", kendangTak(rng)},
		{"siren.wav", siren()},
		{"hoover.wav", hoover()},
		{"stab.wav", stab()},
		{"rev_cymbal.wav", reverseCymbal(rng)},
		{"zap.wav", zap()},
	}
}

func kickHard(rng *rand.Rand) []float64 {
	n := length(0.45)
	out := make([]float64, n)
	click := highPass(noise(rng, n), 2000)
	phase := 0.0
	for i := range out {
		t := seconds(i)
		phase += tau * (48 + 160*math.Exp(-t/0.03)) / rate
		out[i] = math.Sin(phase)*math.Exp(-t/0.18) + 0.5*click[i]*math.Exp(-t/0.003)
	}
	return finish(drive(out, 4), 0.95)
}

func snareHard(rng *rand.Rand) []float64 {
	n := length(0.3)
	out := make([]float64, n)
	rattle := bandPass(noise(rng, n), 3500, 0.8)
	phase := 0.0
	for i := range out {
		t := seconds(i)
		phase += tau * (180 + 50*math.Exp(-t/0.02)) / rate
		out[i] = 0.6*math.Sin(phase)*math.Exp(-t/0.06) + 2*rattle[i]*math.Exp(-t/0.09)
	}
	return finish(drive(out, 2.5), 0.9)
}

// kendang is a pitched hand-drum membrane. drop bends the pitch down from
// base+drop at the hit; rise bends it up by that fraction, like a pressed head.
func kendang(rng *rand.Rand, base, drop, rise, decay float64) []float64 {
	n := length(decay * 2.5)
	out := make([]float64, n)
	slap := lowPass(noise(rng, n), 1500)
	phase, overtone := 0.0, 0.0
	for i := range out {
		t := seconds(i)
		f := base + drop*math.Exp(-t/0.05)
		if rise > 0 {
			f *= 1 + rise*(1-math.Exp(-t/0.04))
		}
		phase += tau * f / rate
		overtone += tau * f * 1.6 / rate
		out[i] = math.Sin(phase)*math.Exp(-t/decay) +
			0.3*math.Sin(overtone)*math.Exp(-t/0.08) +
			0.4*slap[i]*math.Exp(-t/0.01)
	}
	return finish(drive(out, 1.5), 0.9)
}

func kendangTak(rng *rand.Rand) []float64 {
	n := length(0.15)
	out := make([]float64, n)
	slap := bandPass(noise(rng, n), 2500, 2)
	for i := range out {
		t := seconds(i)
		out[i] = 3*slap[i]*math.Exp(-t/0.02) + 0.5*math.Sin(tau*520*t)*math.Exp(-t/0.015)
	}
	return finish(drive(out, 2), 0.85)
}

// siren repeats a rising whoop, the DJ siren funkot drops in before a break.
func siren() []float64 {
	n := length(1.6)
	out := make([]float64, n)
	phase := 0.0
	for i := range out {
		t := seconds(i)
		sweep := t*2.5 - math.Floor(t*2.5)
		phase += tau * 500 * math.Pow(2, 1.2*sweep) / rate
		out[i] = math.Tanh(3*math.Sin(phase)) * envelope(t, 0.01, 1.6, 0.2)
	}
	return finish(lowPass(out, 4000), 0.7)
}

// hoover is a detuned saw lead on A3 that dips and glides up into pitch.
func hoover() []float64 {
	n := length(0.7)
	out := make([]float64, n)
	detune := []float64{-15, -7, 0, 7, 15}
	phases := make([]float64, len(detune)+1)
	for i := range out {
		t := seconds(i)
		f := 220 * math.Pow(2, -7.0/12*math.Exp(-t/0.06))
		sum := 0.0
		for v, cents := range detune {
			phases[v] += f * math.Pow(2, cents/1200) / rate
			sum += saw(phases[v])
		}
		phases[len(detune)] += f / 2 / rate
		sum += 0.8 * saw(phases[len(detune)])
		out[i] = sum / 4 * envelope(t, 0.005, 0.7, 0.15)
	}
	return finish(drive(lowPass(out, 2500), 1.5), 0.8)
}

// stab is a short A minor chord hit.
func stab() []float64 {
	n := length(0.35)
	out := make([]float64, n)
	notes := []float64{220, 261.63, 329.63, 440}
	phases := make([]float64, len(notes)*2)
	for i := range out {
		t := seconds(i)
		sum := 0.0
		for v, f := range notes {
			phases[2*v] += f * math.Pow(2, 6.0/1200) / rate
			phases[2*v+1] += f * math.Pow(2, -6.0/1200) / rate
			sum += saw(phases[2*v]) + saw(phases[2*v+1])
		}
		out[i] = sum / 8 * math.Exp(-t/0.12)
	}
	return finish(drive(lowPass(out, 3000), 1.3), 0.8)
}

// reverseCymbal swells for one bar at 180 BPM and cuts on the downbeat.
func reverseCymbal(rng *rand.Rand) []float64 {
	n := length(16 * 60.0 / (180 * 4))
	raw := noise(rng, n)
	hiss := highPass(raw, 6000)
	shimmer := bandPass(raw, 9000, 1.5)
	out := make([]float64, n)
	for i := range out {
		x := float64(i) / float64(n)
		out[i] = (hiss[i] + 2*shimmer[i]) * x * x * x
	}
	return finish(out, 0.7)
}

func zap() []float64 {
	n := length(0.25)
	out := make([]float64, n)
	phase := 0.0
	for i := range out {
		t := seconds(i)
		phase += tau * (80 + 3000*math.Exp(-t/0.04)) / rate
		out[i] = math.Sin(phase) * math.Exp(-t/0.1)
	}
	return finish(drive(out, 2), 0.8)
}

const tau = 2 * math.Pi

func length(seconds float64) int { return int(seconds * rate) }

func seconds(i int) float64 { return float64(i) / rate }

func saw(phase float64) float64 { return 2*(phase-math.Floor(phase)) - 1 }

func noise(rng *rand.Rand, n int) []float64 {
	out := make([]float64, n)
	for i := range out {
		out[i] = rng.Float64()*2 - 1
	}
	return out
}

// envelope rises over attack, holds, and falls over the final release seconds
// of a sound total seconds long.
func envelope(t, attack, total, release float64) float64 {
	return math.Min(1, math.Min(t/attack, (total-t)/release))
}

func lowPass(in []float64, cutoff float64) []float64 {
	alpha := 1 - math.Exp(-tau*cutoff/rate)
	out := make([]float64, len(in))
	y := 0.0
	for i, x := range in {
		y += alpha * (x - y)
		out[i] = y
	}
	return out
}

func highPass(in []float64, cutoff float64) []float64 {
	low := lowPass(in, cutoff)
	out := make([]float64, len(in))
	for i := range in {
		out[i] = in[i] - low[i]
	}
	return out
}

// bandPass is the RBJ cookbook band-pass biquad with 0 dB peak gain.
func bandPass(in []float64, center, q float64) []float64 {
	w := tau * center / rate
	alpha := math.Sin(w) / (2 * q)
	a0 := 1 + alpha
	b0, b2 := alpha/a0, -alpha/a0
	a1, a2 := -2*math.Cos(w)/a0, (1-alpha)/a0
	out := make([]float64, len(in))
	var x1, x2, y1, y2 float64
	for i, x := range in {
		y := b0*x + b2*x2 - a1*y1 - a2*y2
		x2, x1 = x1, x
		y2, y1 = y1, y
		out[i] = y
	}
	return out
}

func drive(in []float64, amount float64) []float64 {
	out := make([]float64, len(in))
	for i, x := range in {
		out[i] = math.Tanh(amount*x) / math.Tanh(amount)
	}
	return out
}

// finish scales the sound to peak and fades its last 5 ms so it ends without a click.
func finish(in []float64, peak float64) []float64 {
	maxAbs := 0.0
	for _, x := range in {
		maxAbs = math.Max(maxAbs, math.Abs(x))
	}
	fade := length(0.005)
	for i := range in {
		in[i] *= peak / maxAbs
		if left := len(in) - 1 - i; left < fade {
			in[i] *= float64(left) / float64(fade)
		}
	}
	return in
}

func encodeWAV(samples []float64) []byte {
	var buf bytes.Buffer
	dataSize := uint32(len(samples) * 2)
	// Writes to a bytes.Buffer cannot fail.
	write := func(v any) { _ = binary.Write(&buf, binary.LittleEndian, v) }
	buf.WriteString("RIFF")
	write(36 + dataSize)
	buf.WriteString("WAVEfmt ")
	write(uint32(16))
	write(uint16(1))
	write(uint16(1))
	write(uint32(rate))
	write(uint32(rate * 2))
	write(uint16(2))
	write(uint16(16))
	buf.WriteString("data")
	write(dataSize)
	for _, x := range samples {
		write(int16(math.Round(math.Max(-1, math.Min(1, x)) * 32767)))
	}
	return buf.Bytes()
}
