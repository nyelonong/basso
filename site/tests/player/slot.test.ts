import { describe, expect, it } from "vitest";
import type { Bar, BarProvider } from "../../src/player/model";
import { PatternSlot } from "../../src/player/slot";

const barOf = (step: number): Bar => ({
  bpm: 120,
  steps: 16,
  hits: [{ kind: "sample", sample: "kick2.wav", step, velocity: 1, pan: 0 }],
});
const fixed = (step: number): BarProvider => () => ({ ok: true, bar: barOf(step) });
const failing: BarProvider = (bar) => ({ ok: false, error: { message: "boom", bar } });

describe("PatternSlot", () => {
  it("returns no bar before anything is applied", () => {
    expect(new PatternSlot().barFor(0)).toEqual({ bar: null });
  });

  it("switches to an applied provider on the next request", () => {
    const slot = new PatternSlot();
    slot.apply(fixed(1));
    expect(slot.barFor(0)).toEqual({ bar: barOf(1) });
    slot.apply(fixed(2));
    expect(slot.barFor(1)).toEqual({ bar: barOf(2) });
  });

  it("keeps the last good bar when the provider fails", () => {
    const slot = new PatternSlot();
    slot.apply(fixed(3));
    slot.barFor(0);
    slot.apply(failing);
    expect(slot.barFor(1)).toEqual({ bar: barOf(3), error: { message: "boom", bar: 1 } });
  });

  it("returns a copy of the last good bar", () => {
    const slot = new PatternSlot();
    slot.apply(fixed(3));
    slot.barFor(0);
    slot.apply(failing);
    const first = slot.barFor(1).bar!;
    (first.hits as unknown[]).length = 0;
    expect(slot.barFor(2).bar).toEqual(barOf(3));
  });

  it("returns no bar when it has never had a good one", () => {
    const slot = new PatternSlot();
    slot.apply(failing);
    expect(slot.barFor(0)).toEqual({ bar: null, error: { message: "boom", bar: 0 } });
  });
});
