import { readFileSync } from "node:fs";
import { resolve } from "node:path";
import { describe, expect, it } from "vitest";
import { createFennelRuntime } from "../../src/player/fennel";
import { emptyGrid } from "../../src/player/grid";
import type { AudioSink, Bar, Diagnostic } from "../../src/player/model";
import { Session } from "../../src/player/session";

const repo = resolve(import.meta.dirname, "../../..");
const runtime = createFennelRuntime(readFileSync(resolve(repo, "internal/engine/fennel/compiler.lua"), "utf8"));
const samples = ["kick2.wav", "snare.wav", "cl_hihat.wav"];

async function harness() {
  let now = 0;
  let tick: (() => void) | null = null;
  const played: { sample: string; time: number }[] = [];
  const diagnostics: (Diagnostic | null)[] = [];
  const bars: { n: number; bar: Bar | null }[] = [];
  const states: boolean[] = [];
  const sink: AudioSink = {
    playSample: (sample, time) => played.push({ sample, time }),
    playNote: () => {},
    stopAll: () => {},
  };
  const session = new Session(
    {
      runtime: await runtime,
      sink,
      clock: { now: () => now },
      timer: {
        every: (_ms, fn) => {
          tick = fn;
          return () => (tick = null);
        },
      },
      sampleNames: samples,
      random: () => 0.5,
    },
    {
      onBar: (n, bar) => bars.push({ n, bar }),
      onDiagnostic: (d) => diagnostics.push(d),
      onState: (playing) => states.push(playing),
    },
  );
  return {
    session,
    played,
    diagnostics,
    bars,
    states,
    advanceTo(t: number) {
      while (now < t) {
        now = Math.min(t, now + 0.025);
        tick?.();
      }
    },
    samplesAfter: (t: number) => played.filter((p) => p.time >= t).map((p) => p.sample),
  };
}

const pattern = (sample: string) => `(fn pattern [bar] [{:step 0 :sample "${sample}"}])`;

describe("Session", () => {
  it("plays nothing and reports nothing before a pattern is chosen", async () => {
    const h = await harness();
    h.session.play();
    h.advanceTo(3);
    expect(h.played).toEqual([]);
    expect(h.diagnostics).toEqual([]);
    expect(h.states).toEqual([true]);
  });

  it("applies new code at the next bar", async () => {
    const h = await harness();
    expect(h.session.updateCode(pattern("kick2.wav"))).toBeNull();
    h.session.play();
    h.advanceTo(1);
    expect(h.session.updateCode(pattern("snare.wav"))).toBeNull();
    h.advanceTo(1.9);
    expect(h.played.map((p) => p.sample)).toEqual(["kick2.wav"]);
    h.advanceTo(2.1);
    expect(h.played.map((p) => p.sample)).toEqual(["kick2.wav", "snare.wav"]);
  });

  it("rejects code that does not compile and keeps the current groove", async () => {
    const h = await harness();
    h.session.updateCode(pattern("kick2.wav"));
    h.session.play();
    const error = h.session.updateCode("(fn pattern [bar]\n  [{:step 0)");
    expect(error?.line).toBeGreaterThan(0);
    expect(h.diagnostics.at(-1)).toEqual(error);
    h.advanceTo(4.1);
    expect(new Set(h.played.map((p) => p.sample))).toEqual(new Set(["kick2.wav"]));
  });

  it("rejects code whose next bar fails validation before it reaches the slot", async () => {
    const h = await harness();
    h.session.updateCode(pattern("kick2.wav"));
    h.session.play();
    expect(h.session.updateCode(pattern("nope.wav"))).toMatchObject({
      message: 'hit 0 sample "nope.wav" is not in the sound inventory',
    });
    h.advanceTo(2.1);
    expect(h.samplesAfter(2)).toEqual(["kick2.wav"]);
  });

  it("keeps the last good bar when a later bar fails, then clears the error", async () => {
    const h = await harness();
    h.session.updateCode(
      '(fn pattern [bar] (if (= bar 2) (error "bar two") [{:step 0 :sample "kick2.wav"}]))',
    );
    h.session.play();
    h.advanceTo(6.1);
    expect(h.played.map((p) => p.sample)).toEqual(["kick2.wav", "kick2.wav", "kick2.wav", "kick2.wav"]);
    expect(h.diagnostics).toEqual([null, expect.objectContaining({ bar: 2 }), null]);
  });

  it("switches between grid and code at bar boundaries", async () => {
    const h = await harness();
    const grid = emptyGrid(samples);
    grid.rows[2].cells[0] = { on: true, velocity: 1 };
    h.session.updateGrid(grid);
    h.session.play();
    h.advanceTo(1);
    h.session.updateCode(pattern("snare.wav"));
    h.advanceTo(3);
    h.session.updateGrid(grid);
    h.advanceTo(4.1);
    expect(h.played.map((p) => p.sample)).toEqual(["cl_hihat.wav", "snare.wav", "cl_hihat.wav"]);
  });

  it("stops and restarts from bar 0", async () => {
    const h = await harness();
    h.session.updateCode(pattern("kick2.wav"));
    h.session.play();
    h.advanceTo(2.1);
    h.session.stop();
    h.session.play();
    expect(h.bars.at(-1)?.n).toBe(0);
    expect(h.states).toEqual([true, false, true]);
  });
});
