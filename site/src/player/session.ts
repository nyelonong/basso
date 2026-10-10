import { fennelProvider } from "./evaluate";
import type { FennelRuntime } from "./fennel";
import { gridProvider } from "./grid";
import type { AudioSink, Bar, BarProvider, Diagnostic, GridPattern } from "./model";
import { Scheduler, type AudioClock, type Timer } from "./scheduler";
import { PatternSlot } from "./slot";

export type SessionDeps = {
  runtime: FennelRuntime;
  sink: AudioSink;
  clock: AudioClock;
  timer: Timer;
  sampleNames: readonly string[];
  random: () => number;
};

export type SessionEvents = {
  onBar(n: number, bar: Bar | null, startTime: number): void;
  onDiagnostic(diagnostic: Diagnostic | null): void;
  onState(playing: boolean): void;
};

export class Session {
  private readonly slot = new PatternSlot();
  private readonly scheduler: Scheduler;
  private showingError = false;
  private errorThisBar = false;

  constructor(
    private readonly deps: SessionDeps,
    private readonly events: SessionEvents,
  ) {
    this.scheduler = new Scheduler(deps.clock, deps.timer, deps.sink, this.slot, {
      onError: (error) => {
        this.errorThisBar = true;
        this.report(error);
      },
      onBar: (n, bar, start) => {
        if (!this.errorThisBar && this.showingError) this.report(null);
        this.errorThisBar = false;
        events.onBar(n, bar, start);
      },
    });
  }

  updateCode(source: string): Diagnostic | null {
    const compiled = this.deps.runtime.compile(source);
    if (!compiled.ok) return this.report(compiled.error);
    return this.apply(fennelProvider(this.deps.runtime, compiled.pattern, this.deps.sampleNames, this.deps.random));
  }

  updateGrid(grid: GridPattern): Diagnostic | null {
    return this.apply(gridProvider(grid, this.deps.sampleNames));
  }

  play(): void {
    this.scheduler.start();
    this.events.onState(true);
  }

  stop(): void {
    this.scheduler.stop();
    this.events.onState(false);
  }

  // Trial-runs the bar it would first play, so a broken pattern is reported now
  // and never replaces the one that is playing.
  private apply(provider: BarProvider): Diagnostic | null {
    const trial = provider(this.scheduler.upcomingBar);
    if (!trial.ok) return this.report(trial.error);
    this.slot.apply(provider);
    return this.report(null);
  }

  private report(diagnostic: Diagnostic | null): Diagnostic | null {
    this.showingError = diagnostic !== null;
    this.events.onDiagnostic(diagnostic);
    return diagnostic;
  }
}
