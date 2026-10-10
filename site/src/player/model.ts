export type Instrument = "bass" | "brass" | "pluck" | "lead" | "pad";

export const INSTRUMENTS: readonly Instrument[] = ["bass", "brass", "pluck", "lead", "pad"];

export type Hit = { step: number; velocity: number; pan: number } & (
  | { kind: "sample"; sample: string }
  | { kind: "note"; note: string; instrument: Instrument; length: number }
);

export type SampleHit = Extract<Hit, { kind: "sample" }>;
export type NoteHit = Extract<Hit, { kind: "note" }>;

export type Bar = { hits: readonly Hit[]; bpm: number; steps: number };

export type Diagnostic = { message: string; line?: number; bar?: number };

export type BarResult = { ok: true; bar: Bar } | { ok: false; error: Diagnostic };

export type BarProvider = (bar: number) => BarResult;

export interface AudioSink {
  playSample(name: string, time: number, velocity: number, pan: number): void;
  playNote(
    note: string,
    instrument: Instrument,
    time: number,
    duration: number,
    velocity: number,
    pan: number,
  ): void;
  stopAll(): void;
}

export type GridCell = { on: boolean; velocity: number };
export type GridRow = { sample: string; cells: GridCell[] };
export type GridPattern = { bpm: number; steps: number; rows: GridRow[] };

export function stepSeconds(bpm: number): number {
  return 60 / (bpm * 4);
}
