import { stepSeconds, type AudioSink, type Bar, type Diagnostic } from "./model";
import type { PatternSlot } from "./slot";

export interface AudioClock {
  now(): number;
}

export interface Timer {
  every(ms: number, fn: () => void): () => void;
}

export type SchedulerEvents = {
  onBar(n: number, bar: Bar | null, startTime: number): void;
  onError(error: Diagnostic): void;
};

const TICK_MS = 25;
const START_DELAY = 0.1;
const LOOKAHEAD = 0.2;
const IDLE_BAR_SECONDS = 16 * stepSeconds(120);

export class Scheduler {
  private cancel: (() => void) | null = null;
  private nextBar = 0;
  private nextBarStart = 0;

  constructor(
    private readonly clock: AudioClock,
    private readonly timer: Timer,
    private readonly sink: AudioSink,
    private readonly slot: PatternSlot,
    private readonly events: SchedulerEvents,
  ) {}

  get upcomingBar(): number {
    return this.cancel ? this.nextBar : 0;
  }

  start(): void {
    if (this.cancel) return;
    this.nextBar = 0;
    this.nextBarStart = this.clock.now() + START_DELAY;
    this.cancel = this.timer.every(TICK_MS, () => this.tick());
    this.tick();
  }

  stop(): void {
    this.cancel?.();
    this.cancel = null;
    this.sink.stopAll();
    this.nextBar = 0;
  }

  private tick(): void {
    const now = this.clock.now();
    // A throttled tab can stall the timer; resync rather than burst-play missed bars.
    if (this.nextBarStart < now) this.nextBarStart = now + START_DELAY;
    if (now >= this.nextBarStart - LOOKAHEAD) this.scheduleBar();
  }

  private scheduleBar(): void {
    const start = this.nextBarStart;
    const { bar, error } = this.slot.barFor(this.nextBar);
    if (error) this.events.onError(error);
    this.events.onBar(this.nextBar, bar, start);
    if (bar) {
      const step = stepSeconds(bar.bpm);
      for (const hit of bar.hits) {
        const time = start + hit.step * step;
        if (hit.kind === "sample") this.sink.playSample(hit.sample, time, hit.velocity, hit.pan);
        else this.sink.playNote(hit.note, hit.instrument, time, hit.length * step, hit.velocity, hit.pan);
      }
    }
    this.nextBarStart = start + (bar ? bar.steps * stepSeconds(bar.bpm) : IDLE_BAR_SECONDS);
    this.nextBar++;
  }
}
