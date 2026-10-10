import { describe, expect, it } from "vitest";
import type { AudioSink, Bar, BarProvider, Diagnostic, Hit, Instrument } from "../../src/player/model";
import { Scheduler, type AudioClock, type Timer } from "../../src/player/scheduler";
import { PatternSlot } from "../../src/player/slot";

type Call =
  | { type: "sample"; name: string; time: number; velocity: number; pan: number }
  | { type: "note"; note: string; instrument: Instrument; time: number; duration: number }
  | { type: "stop" };

function harness() {
  let now = 10;
  let tick: (() => void) | null = null;
  let cancelled = 0;
  const calls: Call[] = [];
  const bars: { n: number; bar: Bar | null; start: number }[] = [];
  const errors: Diagnostic[] = [];
  const clock: AudioClock = { now: () => now };
  const timer: Timer = {
    every: (_ms, fn) => {
      tick = fn;
      return () => {
        tick = null;
        cancelled++;
      };
    },
  };
  const sink: AudioSink = {
    playSample: (name, time, velocity, pan) => calls.push({ type: "sample", name, time, velocity, pan }),
    playNote: (note, instrument, time, duration) => calls.push({ type: "note", note, instrument, time, duration }),
    stopAll: () => calls.push({ type: "stop" }),
  };
  const slot = new PatternSlot();
  const scheduler = new Scheduler(clock, timer, sink, slot, {
    onBar: (n, bar, start) => bars.push({ n, bar, start }),
    onError: (error) => errors.push(error),
  });
  return {
    slot,
    scheduler,
    calls,
    bars,
    errors,
    get cancelled() {
      return cancelled;
    },
    get ticking() {
      return tick !== null;
    },
    jumpTo(t: number) {
      now = t;
      tick?.();
    },
    advanceTo(t: number) {
      while (now < t) {
        now = Math.min(t, now + 0.025);
        tick?.();
      }
    },
  };
}

const sample = (step: number, name = "kick2.wav"): Hit => ({ kind: "sample", sample: name, step, velocity: 0.8, pan: -0.5 });
const provider = (hits: Hit[], bpm = 120, steps = 16): BarProvider => () => ({ ok: true, bar: { hits, bpm, steps } });
const sampleTimes = (calls: Call[], name = "kick2.wav") =>
  calls.flatMap((c) => (c.type === "sample" && c.name === name ? [c.time] : []));

describe("Scheduler", () => {
  it("schedules hits on the step grid at 120 BPM", () => {
    const h = harness();
    h.slot.apply(provider([sample(0), sample(4), sample(15)]));
    h.scheduler.start();
    expect(h.bars[0]).toMatchObject({ n: 0, start: 10.1 });
    expect(sampleTimes(h.calls)).toEqual([10.1, 10.6, 10.1 + 15 * 0.125]);
    expect(h.calls[0]).toMatchObject({ velocity: 0.8, pan: -0.5 });
  });

  it("uses 60 / (bpm * 4) seconds per step", () => {
    const h = harness();
    h.slot.apply(provider([sample(1)], 150));
    h.scheduler.start();
    expect(sampleTimes(h.calls)[0]).toBeCloseTo(10.1 + 0.1, 9);
  });

  it("schedules the next bar shortly before the boundary", () => {
    const h = harness();
    h.slot.apply(provider([sample(0)]));
    h.scheduler.start();
    h.advanceTo(11.875);
    expect(h.bars).toHaveLength(1);
    h.advanceTo(11.925);
    expect(h.bars.map((b) => b.start)).toEqual([10.1, 12.1]);
  });

  it("applies a new pattern at the next bar, never mid-bar", () => {
    const h = harness();
    h.slot.apply(provider([sample(0)]));
    h.scheduler.start();
    h.advanceTo(11);
    h.slot.apply(provider([sample(2, "snare.wav")]));
    h.advanceTo(11.5);
    expect(sampleTimes(h.calls, "snare.wav")).toEqual([]);
    h.advanceTo(12.1);
    expect(sampleTimes(h.calls, "snare.wav")).toEqual([12.1 + 2 * 0.125]);
    expect(sampleTimes(h.calls)).toEqual([10.1]);
  });

  it("keeps playing the last good bar and reports errors", () => {
    const h = harness();
    h.slot.apply(provider([sample(0)]));
    h.scheduler.start();
    h.slot.apply((bar) => ({ ok: false, error: { message: "boom", bar } }));
    h.advanceTo(12.1);
    expect(sampleTimes(h.calls)).toEqual([10.1, 12.1]);
    expect(h.errors).toEqual([{ message: "boom", bar: 1 }]);
  });

  it("starts the bar after a tempo change where the old bar ends", () => {
    const h = harness();
    h.slot.apply(provider([sample(0)], 120));
    h.scheduler.start();
    h.slot.apply(provider([sample(0)], 60));
    h.advanceTo(16.1);
    expect(h.bars.map((b) => b.start)).toEqual([10.1, 12.1, 16.1]);
  });

  it("uses the bar's own step count for its length", () => {
    const h = harness();
    h.slot.apply(provider([sample(0)], 120, 8));
    h.scheduler.start();
    h.advanceTo(11.1);
    expect(h.bars.map((b) => b.start)).toEqual([10.1, 11.1]);
  });

  it("schedules notes for length * step seconds", () => {
    const h = harness();
    h.slot.apply(
      provider([{ kind: "note", note: "C2", instrument: "bass", length: 3, step: 2, velocity: 1, pan: 0 }]),
    );
    h.scheduler.start();
    expect(h.calls).toEqual([{ type: "note", note: "C2", instrument: "bass", time: 10.35, duration: 0.375 }]);
  });

  it("keeps time with no pattern by counting empty 16-step bars at 120 BPM", () => {
    const h = harness();
    h.scheduler.start();
    h.advanceTo(12.1);
    expect(h.bars.map((b) => [b.n, b.bar, b.start])).toEqual([
      [0, null, 10.1],
      [1, null, 12.1],
    ]);
  });

  it("stops at once and restarts from bar 0", () => {
    const h = harness();
    h.slot.apply(provider([sample(0)]));
    h.scheduler.start();
    h.advanceTo(12.1);
    h.scheduler.stop();
    expect(h.calls.at(-1)).toEqual({ type: "stop" });
    expect(h.ticking).toBe(false);
    h.advanceTo(13);
    h.scheduler.start();
    expect(h.bars.at(-1)).toMatchObject({ n: 0, start: 13.1 });
  });

  it("catches up after the timer stalls instead of piling up old bars", () => {
    const h = harness();
    h.slot.apply(provider([sample(0)]));
    h.scheduler.start();
    h.jumpTo(30);
    expect(h.bars).toHaveLength(2);
    expect(h.bars[1].start).toBeGreaterThanOrEqual(30);
  });
});
