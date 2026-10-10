import { readFileSync } from "node:fs";
import { resolve } from "node:path";
import { describe, expect, it } from "vitest";
import { createFennelRuntime } from "../../src/player/fennel";
import { emptyGrid, gridProvider, gridToBar, gridToFennel } from "../../src/player/grid";
import type { GridPattern } from "../../src/player/model";

const repo = resolve(import.meta.dirname, "../../..");
const compilerSource = readFileSync(resolve(repo, "internal/engine/fennel/compiler.lua"), "utf8");
const samples = ["kick2.wav", "snare.wav", "cl_hihat.wav", "handclap.wav"];

function grid(): GridPattern {
  const g = emptyGrid(samples, 16, 132);
  g.rows[0].cells[0] = { on: true, velocity: 1 };
  g.rows[0].cells[8] = { on: true, velocity: 0.7 };
  g.rows[1].cells[4] = { on: true, velocity: 0.4 };
  g.rows[2].cells[15] = { on: true, velocity: 0.7 };
  g.rows[3].cells[12] = { on: false, velocity: 1 };
  return g;
}

describe("grid conversion", () => {
  it("creates an empty grid with one row per sample", () => {
    const g = emptyGrid(["kick2.wav"]);
    expect(g).toEqual({
      bpm: 120,
      steps: 16,
      rows: [{ sample: "kick2.wav", cells: Array.from({ length: 16 }, () => ({ on: false, velocity: 1 })) }],
    });
  });

  it("turns cells that are on into centered sample hits", () => {
    expect(gridToBar(grid())).toEqual({
      bpm: 132,
      steps: 16,
      hits: [
        { kind: "sample", sample: "kick2.wav", step: 0, velocity: 1, pan: 0 },
        { kind: "sample", sample: "kick2.wav", step: 8, velocity: 0.7, pan: 0 },
        { kind: "sample", sample: "snare.wav", step: 4, velocity: 0.4, pan: 0 },
        { kind: "sample", sample: "cl_hihat.wav", step: 15, velocity: 0.7, pan: 0 },
      ],
    });
  });

  it("provides the same validated bar for every bar number", () => {
    const provide = gridProvider(grid(), samples);
    expect(provide(0)).toEqual({ ok: true, bar: gridToBar(grid()) });
    expect(provide(7)).toEqual({ ok: true, bar: gridToBar(grid()) });
  });

  it("reports an invalid grid", () => {
    const bad = { ...grid(), bpm: 999 };
    expect(gridProvider(bad, samples)(2)).toEqual({
      ok: false,
      error: { message: "BPM 999 must be in [20,400]", bar: 2 },
    });
  });

  it("converts to Fennel that plays exactly the same hits", async () => {
    const runtime = await createFennelRuntime(compilerSource);
    const source = gridToFennel(grid());
    const compiled = runtime.compile(source);
    if (!compiled.ok) throw new Error(compiled.error.message);
    const result = runtime.run(compiled.pattern, 3);
    if (!result.ok) throw new Error(result.error.message);
    const bar = gridToBar(grid());
    expect({ bpm: result.bpm, steps: result.steps }).toEqual({ bpm: bar.bpm, steps: bar.steps });
    expect(result.rawHits).toEqual(
      bar.hits.map((h) => ({ step: h.step, sample: h.kind === "sample" ? h.sample : "", velocity: h.velocity, pan: 0 })),
    );
  });

  it("converts an empty grid to valid Fennel with no hits", async () => {
    const runtime = await createFennelRuntime(compilerSource);
    const compiled = runtime.compile(gridToFennel(emptyGrid(samples)));
    if (!compiled.ok) throw new Error(compiled.error.message);
    expect(runtime.run(compiled.pattern, 0)).toEqual({ ok: true, bpm: 120, steps: 16, rawHits: [] });
  });

  it("writes readable Fennel", () => {
    expect(gridToFennel(grid())).toBe(
      [
        "(bpm 132)",
        "(steps 16)",
        "",
        "(fn pattern [bar]",
        '  [{:step 0 :sample "kick2.wav" :velocity 1 :pan 0}',
        '   {:step 8 :sample "kick2.wav" :velocity 0.7 :pan 0}',
        '   {:step 4 :sample "snare.wav" :velocity 0.4 :pan 0}',
        '   {:step 15 :sample "cl_hihat.wav" :velocity 0.7 :pan 0}])',
        "",
        "pattern",
        "",
      ].join("\n"),
    );
  });
});
