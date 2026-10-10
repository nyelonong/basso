import type { CompiledPattern, FennelRuntime } from "./fennel";
import type { Bar, BarProvider, Hit, Instrument } from "./model";
import { validateBar } from "./validate";

class HitError extends Error {}

// Mirrors gopher-lua's LVAsString: numbers become strings, other types become "".
function asString(value: unknown): string {
  if (typeof value === "string") return value;
  if (typeof value === "number") return String(value);
  return "";
}

function numberField(row: Record<string, unknown>, field: string, hit: number): number | undefined {
  const value = row[field];
  if (value === undefined || value === null) return undefined;
  if (typeof value !== "number") throw new HitError(`hit ${hit} :${field} must be a number`);
  return value;
}

// Converts one raw hit with the defaults the Go engine applies in fennel.go.
function toHit(row: Record<string, unknown>, hit: number, random: () => number): Hit {
  const hasSample = row.sample !== undefined && row.sample !== null;
  const hasNote = row.note !== undefined && row.note !== null;
  if (hasSample === hasNote) throw new HitError(`hit ${hit} must have exactly one of :sample or :note`);

  const step = numberField(row, "step", hit);
  if (step === undefined) throw new HitError(`hit ${hit} :step is required`);
  const pan = numberField(row, "pan", hit) ?? (hasNote ? 0 : random() * 2 - 1);
  const velocity = numberField(row, "velocity", hit) ?? 1;
  const length = Math.trunc(numberField(row, "length", hit) ?? 1);
  const common = { step: Math.trunc(step), velocity, pan };

  if (hasSample) return { kind: "sample", sample: asString(row.sample), ...common };
  const instrument = row.instrument === undefined || row.instrument === null ? "bass" : asString(row.instrument);
  return { kind: "note", note: asString(row.note), instrument: instrument as Instrument, length, ...common };
}

export function fennelProvider(
  runtime: FennelRuntime,
  pattern: CompiledPattern,
  sampleNames: readonly string[],
  random: () => number,
): BarProvider {
  return (barNumber) => {
    const result = runtime.run(pattern, barNumber);
    if (!result.ok) return result;
    let hits: Hit[];
    try {
      hits = result.rawHits.map((row, index) => toHit(row, index + 1, random));
    } catch (error) {
      if (!(error instanceof HitError)) throw error;
      return { ok: false, error: { message: error.message, bar: barNumber } };
    }
    const emptyName = hits.findIndex((hit) => (hit.kind === "sample" ? hit.sample : hit.note) === "");
    if (emptyName >= 0) {
      return {
        ok: false,
        error: { message: `hit ${emptyName} must have exactly one of sample or note`, bar: barNumber },
      };
    }
    const bar: Bar = { hits, bpm: result.bpm, steps: result.steps };
    const invalid = validateBar(bar, sampleNames);
    if (invalid !== null) return { ok: false, error: { message: invalid, bar: barNumber } };
    return { ok: true, bar };
  };
}
