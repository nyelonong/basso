import { describe, expect, it } from "vitest";
import type { Bar, Hit, NoteHit, SampleHit } from "../../src/player/model";
import { validateBar } from "../../src/player/validate";

const samples = ["kick2.wav", "snare.wav"];
const kick = (step = 0): SampleHit => ({ kind: "sample", sample: "kick2.wav", step, velocity: 1, pan: 0 });
const note = (step: number, instrument: "bass" | "lead" | "pad", length = 1): NoteHit => ({
  kind: "note",
  note: "C3",
  instrument,
  length,
  step,
  velocity: 1,
  pan: 0,
});
const bar = (hits: Hit[], over: Partial<Bar> = {}): Bar => ({ hits, bpm: 120, steps: 16, ...over });

describe("validateBar", () => {
  it("accepts a valid bar", () => {
    expect(validateBar(bar([kick(0), note(4, "bass", 2)]), samples)).toBeNull();
  });

  it.each([
    [bar([], { bpm: 19 }), "BPM 19 must be in [20,400]"],
    [bar([], { bpm: 401 }), "BPM 401 must be in [20,400]"],
    [bar([], { steps: 0 }), "steps per bar 0 must be in [1,256]"],
    [bar([], { steps: 257 }), "steps per bar 257 must be in [1,256]"],
    [bar(Array.from({ length: 4097 }, () => kick())), "hit count 4097 exceeds 4096"],
    [bar([kick(16)]), "hit 0 step 16 is outside [0,16)"],
    [bar([kick(-1)]), "hit 0 step -1 is outside [0,16)"],
    [bar([{ ...kick(), pan: 1.01 }]), "hit 0 pan must be finite and in [-1,1]"],
    [bar([{ ...kick(), pan: Number.NaN }]), "hit 0 pan must be finite and in [-1,1]"],
    [bar([{ ...kick(), velocity: -0.1 }]), "hit 0 velocity must be finite and in [0,1]"],
    [bar([{ ...kick(), velocity: Number.POSITIVE_INFINITY }]), "hit 0 velocity must be finite and in [0,1]"],
    [bar([{ ...kick(), sample: "../kick2.wav" }]), 'hit 0 sample "../kick2.wav" must be a basename'],
    [bar([{ ...kick(), sample: ".." }]), 'hit 0 sample ".." must be a basename'],
    [bar([{ ...kick(), sample: "cowbell.wav" }]), 'hit 0 sample "cowbell.wav" is not in the sound inventory'],
    [bar([{ ...note(0, "bass"), note: "H2" }]), 'hit 0 note "H2" is invalid: invalid note name "H2"'],
    [bar([note(0, "bass", 0)]), "hit 0 note length 0 must be in [1,4096]"],
    [bar([note(0, "bass", 4097)]), "hit 0 note length 4097 must be in [1,4096]"],
    [bar([{ ...note(0, "bass"), instrument: "kazoo" as "bass" }]), 'hit 0 instrument "kazoo" is not supported'],
    [bar([note(12, "lead", 5)]), 'hit 0 instrument "lead" must end within its bar'],
  ])("rejects %#: %s", (input, message) => {
    expect(validateBar(input, samples)).toBe(message);
  });

  it("lets a bass note ring past the bar end", () => {
    expect(validateBar(bar([note(12, "bass", 8)]), samples)).toBeNull();
  });

  it("reports the first failing hit", () => {
    expect(validateBar(bar([kick(0), kick(20), kick(-1)]), samples)).toBe("hit 1 step 20 is outside [0,16)");
  });

  it("allows 64 lead/pad hits and rejects 65", () => {
    const hits = (n: number) => Array.from({ length: n }, (_, i) => note(i % 16, i % 2 ? "lead" : "pad"));
    expect(validateBar(bar(hits(64)), samples)).toBeNull();
    const spread = (n: number) => Array.from({ length: n }, (_, i) => note(i % 256, "lead"));
    expect(validateBar(bar(spread(64), { steps: 256 }), samples)).toBeNull();
    expect(validateBar(bar(spread(65), { steps: 256 }), samples)).toBe("lead/pad hit count 65 exceeds 64");
  });

  it("allows 8 overlapping lead/pad voices and rejects 9", () => {
    const voices = (n: number) => Array.from({ length: n }, (_, i) => note(0, i % 2 ? "lead" : "pad", 4));
    expect(validateBar(bar(voices(8)), samples)).toBeNull();
    expect(validateBar(bar(voices(9)), samples)).toBe("lead/pad overlap 9 at step 0 exceeds 8 voices");
  });

  it("does not count bass notes against the lead/pad limits", () => {
    const hits = Array.from({ length: 20 }, () => note(0, "bass", 4));
    expect(validateBar(bar(hits), samples)).toBeNull();
  });

  it("frees a voice when a note ends", () => {
    const hits = [...Array.from({ length: 8 }, () => note(0, "lead", 2)), ...Array.from({ length: 8 }, () => note(2, "pad", 2))];
    expect(validateBar(bar(hits), samples)).toBeNull();
  });
});
