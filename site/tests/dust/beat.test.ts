import { describe, expect, it } from "vitest";
import { BeatQueue, HAT, KICK, SNARE, TONE, voiceOf } from "../../src/dust/beat";
import { SAMPLE_NAMES } from "../../src/player/assets";

describe("voiceOf", () => {
  it("sorts the 808 kit into the dust's four voices", () => {
    expect(voiceOf("kick1.wav")).toBe(KICK);
    expect(voiceOf("kick2.wav")).toBe(KICK);
    expect(voiceOf("snare.wav")).toBe(SNARE);
    expect(voiceOf("handclap.wav")).toBe(SNARE);
    expect(voiceOf("rimshot.wav")).toBe(SNARE);
    expect(voiceOf("cl_hihat.wav")).toBe(HAT);
    expect(voiceOf("open_hh.wav")).toBe(HAT);
    expect(voiceOf("crashcym.wav")).toBe(HAT);
    expect(voiceOf("tom1.wav")).toBe(TONE);
  });

  it("gives every bundled sample a voice", () => {
    for (const name of SAMPLE_NAMES) expect([KICK, SNARE, HAT, TONE]).toContain(voiceOf(name));
  });
});

describe("BeatQueue", () => {
  it("stays silent until a hit's time arrives", () => {
    const beat = new BeatQueue();
    beat.add(KICK, 2, 0.8);
    expect(beat.envelopes(1.99)).toEqual([0, 0, 0, 0]);
    expect(beat.envelopes(2)[KICK]).toBeCloseTo(0.8);
  });

  it("decays after the hit, the hi-hat fastest", () => {
    const beat = new BeatQueue();
    beat.add(KICK, 1, 1);
    beat.add(HAT, 1, 1);
    const [kick, , hat] = beat.envelopes(1.1);
    expect(kick).toBeGreaterThan(hat);
    expect(kick).toBeLessThan(1);
    expect(beat.envelopes(5)[KICK]).toBeLessThan(0.001);
  });

  it("takes hits added out of order in time order", () => {
    const beat = new BeatQueue();
    beat.add(SNARE, 3, 0.5);
    beat.add(SNARE, 2, 0.9);
    expect(beat.envelopes(2.001)[SNARE]).toBeCloseTo(0.9, 1);
    expect(beat.envelopes(3)[SNARE]).toBeCloseTo(0.5);
  });

  it("keeps the louder of two hits on one voice at the same time", () => {
    const beat = new BeatQueue();
    beat.add(KICK, 1, 0.4);
    beat.add(KICK, 1, 0.9);
    expect(beat.envelopes(1)[KICK]).toBeCloseTo(0.9);
  });

  it("reports the time since the last kick", () => {
    const beat = new BeatQueue();
    expect(beat.kickAge(4)).toBe(Infinity);
    beat.add(KICK, 1, 1);
    beat.envelopes(1.5);
    expect(beat.kickAge(1.5)).toBeCloseTo(0.5);
  });

  it("forgets everything on stop", () => {
    const beat = new BeatQueue();
    beat.add(KICK, 1, 1);
    beat.add(KICK, 2, 1);
    beat.envelopes(1);
    beat.clear();
    expect(beat.envelopes(2)).toEqual([0, 0, 0, 0]);
    expect(beat.kickAge(2)).toBe(Infinity);
  });
});
