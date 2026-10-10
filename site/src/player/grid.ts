import type { Bar, BarProvider, GridPattern } from "./model";
import { validateBar } from "./validate";

export function emptyGrid(samples: readonly string[], steps = 16, bpm = 120): GridPattern {
  return {
    bpm,
    steps,
    rows: samples.map((sample) => ({
      sample,
      cells: Array.from({ length: steps }, () => ({ on: false, velocity: 1 })),
    })),
  };
}

export function gridToBar(grid: GridPattern): Bar {
  return {
    bpm: grid.bpm,
    steps: grid.steps,
    hits: grid.rows.flatMap((row) =>
      row.cells.flatMap((cell, step) =>
        cell.on ? [{ kind: "sample" as const, sample: row.sample, step, velocity: cell.velocity, pan: 0 }] : [],
      ),
    ),
  };
}

export function gridProvider(grid: GridPattern, sampleNames: readonly string[]): BarProvider {
  const bar = gridToBar(grid);
  const invalid = validateBar(bar, sampleNames);
  return (n) => (invalid === null ? { ok: true, bar } : { ok: false, error: { message: invalid, bar: n } });
}

export function gridToFennel(grid: GridPattern): string {
  const hits = gridToBar(grid).hits.map((hit) =>
    hit.kind === "sample"
      ? `{:step ${hit.step} :sample ${JSON.stringify(hit.sample)} :velocity ${hit.velocity} :pan 0}`
      : "",
  );
  const body = hits.length === 0 ? "  [])" : `  [${hits.join("\n   ")}])`;
  return `(bpm ${grid.bpm})\n(steps ${grid.steps})\n\n(fn pattern [bar]\n${body}\n\npattern\n`;
}
