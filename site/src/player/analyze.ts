import { MASTER_OUTPUT_TRIM, SOFT_CLIP_KNEE } from "./audio";
import { stepSeconds, type Bar } from "./model";
import type { RenderedAudio } from "./render";

export type Bands = { lowDb: number; midDb: number; highDb: number };

export type Analysis = {
  seconds: number;
  peakDb: number;
  rmsDb: number;
  clippedSamples: number;
  clippedPercent: number;
  limitedPercent: number;
  bands: Bands;
  bars: ({ bar: number; rmsDb: number } & Bands)[];
  warnings: string[];
};

// Band edges in Hz. One-pole filters make the split approximate: good for
// comparing bars and spotting a dull top or a muddy low end, not for mastering.
const LOW_EDGE = 200;
const HIGH_EDGE = 4000;
// Output level where the master bus soft clipper starts bending the signal:
// its knee, scaled by the trim that follows it.
export const LIMITED_LEVEL = SOFT_CLIP_KNEE * MASTER_OUTPUT_TRIM;
const SILENCE_DB = -60;

const toDb = (level: number) => Math.round(20 * Math.log10(Math.max(level, 1e-6)) * 10) / 10;

function lowPass(input: Float32Array, cutoff: number, sampleRate: number): Float32Array {
  const alpha = 1 - Math.exp((-2 * Math.PI * cutoff) / sampleRate);
  const out = new Float32Array(input.length);
  let y = 0;
  for (let i = 0; i < input.length; i++) {
    y += alpha * (input[i] - y);
    out[i] = y;
  }
  return out;
}

function rms(values: Float32Array, from: number, to: number): number {
  let sum = 0;
  for (let i = from; i < to; i++) sum += values[i] * values[i];
  return to > from ? Math.sqrt(sum / (to - from)) : 0;
}

// Measures rendered audio the way a mix engineer would glance at a meter:
// level, headroom, how hard the limiter worked, and where the energy sits per bar.
export function analyzeAudio(audio: RenderedAudio, bars: readonly Bar[]): Analysis {
  const channels = Array.from({ length: audio.numberOfChannels }, (_, c) => audio.getChannelData(c));
  const frames = audio.length;
  const mono = new Float32Array(frames);
  let peak = 0;
  let clipped = 0;
  let limited = 0;
  for (const data of channels) {
    for (let i = 0; i < frames; i++) {
      const level = Math.abs(data[i]);
      if (level > peak) peak = level;
      if (level >= 0.999) clipped++;
      if (level > LIMITED_LEVEL) limited++;
      mono[i] += data[i] / channels.length;
    }
  }

  const low = lowPass(mono, LOW_EDGE, audio.sampleRate);
  const belowHigh = lowPass(mono, HIGH_EDGE, audio.sampleRate);
  const mid = belowHigh.map((value, i) => value - low[i]);
  const high = mono.map((value, i) => value - belowHigh[i]);
  const bandsBetween = (from: number, to: number): Bands => ({
    lowDb: toDb(rms(low, from, to)),
    midDb: toDb(rms(mid, from, to)),
    highDb: toDb(rms(high, from, to)),
  });

  const perBar: Analysis["bars"] = [];
  let start = 0;
  for (const [index, bar] of bars.entries()) {
    const end = Math.min(frames, Math.round(start + bar.steps * stepSeconds(bar.bpm) * audio.sampleRate));
    perBar.push({ bar: index, rmsDb: toDb(rms(mono, start, end)), ...bandsBetween(start, end) });
    start = end;
  }

  const samples = Math.max(1, frames * channels.length);
  const analysis: Analysis = {
    seconds: frames / audio.sampleRate,
    peakDb: toDb(peak),
    rmsDb: toDb(rms(mono, 0, frames)),
    clippedSamples: clipped,
    clippedPercent: Math.round((10000 * clipped) / samples) / 100,
    limitedPercent: Math.round((10000 * limited) / samples) / 100,
    bands: bandsBetween(0, frames),
    bars: perBar,
    warnings: [],
  };
  analysis.warnings = warningsFor(analysis);
  return analysis;
}

function warningsFor(a: Analysis): string[] {
  const warnings: string[] = [];
  if (a.peakDb <= SILENCE_DB) return ["silent: nothing above -60 dB; check that hits exist and velocities are above 0"];
  if (a.clippedSamples > 0) {
    warnings.push(`clipping: ${a.clippedSamples} samples at full scale; lower the loudest velocities`);
  }
  if (a.limitedPercent > 1) {
    warnings.push(`limiter working hard: ${a.limitedPercent}% of samples in the soft clipper; lower the loudest velocities`);
  }
  if (a.bands.lowDb - a.bands.highDb > 20) {
    warnings.push(`dull top: highs are ${Math.round(a.bands.lowDb - a.bands.highDb)} dB under the lows; raise hats, shakers or cymbals`);
  }
  if (a.bands.highDb - a.bands.lowDb > 6) {
    warnings.push(`thin low end: lows are ${Math.round(a.bands.highDb - a.bands.lowDb)} dB under the highs; add kick or bass`);
  }
  return warnings;
}
