import { INSTRUMENTS, type Bar, type Instrument } from "./model";
import { noteFrequency } from "./notes";

// Mirrors ValidateBar in internal/engine/validation.go and the lead/pad group
// policy in internal/engine/palette.go.
const ELECTRONIC: ReadonlySet<Instrument> = new Set(["lead", "pad"]);
const ELECTRONIC_MAX_HITS = 64;
const ELECTRONIC_MAX_VOICES = 8;

const finiteIn = (value: number, min: number, max: number) =>
  Number.isFinite(value) && value >= min && value <= max;

export function validateBar(bar: Bar, sampleNames: readonly string[]): string | null {
  if (bar.bpm < 20 || bar.bpm > 400) return `BPM ${bar.bpm} must be in [20,400]`;
  if (bar.steps < 1 || bar.steps > 256) return `steps per bar ${bar.steps} must be in [1,256]`;
  if (bar.hits.length > 4096) return `hit count ${bar.hits.length} exceeds 4096`;

  let electronicHits = 0;
  const changes = new Array<number>(bar.steps + 1).fill(0);

  for (const [index, hit] of bar.hits.entries()) {
    if (hit.step < 0 || hit.step >= bar.steps) {
      return `hit ${index} step ${hit.step} is outside [0,${bar.steps})`;
    }
    if (!finiteIn(hit.pan, -1, 1)) return `hit ${index} pan must be finite and in [-1,1]`;
    if (!finiteIn(hit.velocity, 0, 1)) return `hit ${index} velocity must be finite and in [0,1]`;

    if (hit.kind === "sample") {
      const name = JSON.stringify(hit.sample);
      if (/[/\\]/.test(hit.sample) || hit.sample === "." || hit.sample === "..") {
        return `hit ${index} sample ${name} must be a basename`;
      }
      if (!sampleNames.includes(hit.sample)) {
        return `hit ${index} sample ${name} is not in the sound inventory`;
      }
      continue;
    }

    try {
      noteFrequency(hit.note);
    } catch (error) {
      return `hit ${index} note ${JSON.stringify(hit.note)} is invalid: ${(error as Error).message}`;
    }
    if (hit.length < 1 || hit.length > 4096) {
      return `hit ${index} note length ${hit.length} must be in [1,4096]`;
    }
    if (!INSTRUMENTS.includes(hit.instrument)) {
      return `hit ${index} instrument ${JSON.stringify(hit.instrument)} is not supported`;
    }
    if (ELECTRONIC.has(hit.instrument)) {
      if (hit.step + hit.length > bar.steps) {
        return `hit ${index} instrument ${JSON.stringify(hit.instrument)} must end within its bar`;
      }
      electronicHits++;
      changes[hit.step]++;
      changes[hit.step + hit.length]--;
    }
  }

  if (electronicHits > ELECTRONIC_MAX_HITS) {
    return `lead/pad hit count ${electronicHits} exceeds ${ELECTRONIC_MAX_HITS}`;
  }
  let active = 0;
  for (let step = 0; step < bar.steps; step++) {
    active += changes[step];
    if (active > ELECTRONIC_MAX_VOICES) {
      return `lead/pad overlap ${active} at step ${step} exceeds ${ELECTRONIC_MAX_VOICES} voices`;
    }
  }
  return null;
}
