import { describe, expect, it } from "vitest";
import { LIMITED_LEVEL, analyzeAudio } from "../../src/player/analyze";
import { MASTER_OUTPUT_TRIM, softClip } from "../../src/player/audio";
import type { Bar } from "../../src/player/model";
import type { RenderedAudio } from "../../src/player/render";

const RATE = 44100;
const bar = (bpm = 120): Bar => ({ bpm, steps: 16, swing: 50, hits: [] });

function audio(...channels: Float32Array[]): RenderedAudio {
  return { numberOfChannels: channels.length, sampleRate: RATE, length: channels[0].length, getChannelData: (c) => channels[c] };
}

const tone = (freq: number, seconds: number, level: number) =>
  Float32Array.from({ length: Math.round(seconds * RATE) }, (_, i) => level * Math.sin((2 * Math.PI * freq * i) / RATE));

describe("analyzeAudio", () => {
  it("reports peak and RMS of a sine in dB", () => {
    const sine = tone(1000, 2, 0.5);
    const result = analyzeAudio(audio(sine, sine), [bar()]);
    expect(result.peakDb).toBeCloseTo(-6, 0);
    expect(result.rmsDb).toBeCloseTo(-9, 0);
    expect(result.seconds).toBe(2);
    expect(result.clippedPercent).toBe(0);
  });

  it("puts a bass tone in the low band and a hiss tone in the high band", () => {
    const bass = analyzeAudio(audio(tone(60, 2, 0.5)), [bar()]).bands;
    const hiss = analyzeAudio(audio(tone(10000, 2, 0.5)), [bar()]).bands;
    expect(bass.lowDb).toBeGreaterThan(bass.highDb + 20);
    expect(hiss.highDb).toBeGreaterThan(hiss.lowDb + 20);
  });

  it("measures each bar on its own, following each bar's tempo", () => {
    // 2 s at 120 BPM, then 1 s at 240 BPM: loud first bar, quiet second.
    const data = new Float32Array(3 * RATE);
    data.set(tone(1000, 2, 0.5), 0);
    data.set(tone(1000, 1, 0.05), 2 * RATE);
    const result = analyzeAudio(audio(data), [bar(120), bar(240)]);
    expect(result.bars.map((b) => b.bar)).toEqual([0, 1]);
    expect(result.bars[0].rmsDb - result.bars[1].rmsDb).toBeCloseTo(20, 0);
  });

  it("counts clipped and limited samples and warns about them", () => {
    const hot = Float32Array.from({ length: RATE }, (_, i) => (i % 2 === 0 ? 1 : 0.9));
    const result = analyzeAudio(audio(hot), [bar(240)]);
    expect(result.clippedSamples).toBe(RATE / 2);
    expect(result.clippedPercent).toBe(50);
    expect(result.limitedPercent).toBe(100);
    expect(result.warnings.join(" ")).toMatch(/clipping/);
    expect(result.warnings.join(" ")).toMatch(/limiter working hard/);
  });

  it("counts as limited exactly the output the soft clipper has bent", () => {
    // The clipper leaves its input alone up to the knee; after the trim that is LIMITED_LEVEL.
    expect(softClip(LIMITED_LEVEL / MASTER_OUTPUT_TRIM)).toBeCloseTo(LIMITED_LEVEL / MASTER_OUTPUT_TRIM, 9);
    expect(softClip(LIMITED_LEVEL / MASTER_OUTPUT_TRIM + 0.05)).toBeLessThan(LIMITED_LEVEL / MASTER_OUTPUT_TRIM + 0.05);
    const justUnder = new Float32Array(RATE).fill(LIMITED_LEVEL - 0.01);
    const justOver = new Float32Array(RATE).fill(LIMITED_LEVEL + 0.01);
    expect(analyzeAudio(audio(justUnder), [bar(240)]).limitedPercent).toBe(0);
    expect(analyzeAudio(audio(justOver), [bar(240)]).limitedPercent).toBe(100);
  });

  it("warns about a dull top and calls silence silent", () => {
    expect(analyzeAudio(audio(tone(60, 2, 0.5)), [bar()]).warnings.join(" ")).toMatch(/dull top/);
    expect(analyzeAudio(audio(new Float32Array(RATE)), [bar(240)]).warnings).toEqual([
      "silent: nothing above -60 dB; check that hits exist and velocities are above 0",
    ]);
  });
});
