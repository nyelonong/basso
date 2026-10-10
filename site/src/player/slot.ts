import type { Bar, BarProvider, Diagnostic } from "./model";

const copyBar = (bar: Bar): Bar => ({ ...bar, hits: bar.hits.map((hit) => ({ ...hit })) });

export class PatternSlot {
  private current: BarProvider | null = null;
  private pending: BarProvider | null = null;
  private lastGood: Bar | null = null;

  apply(provider: BarProvider): void {
    this.pending = provider;
  }

  barFor(n: number): { bar: Bar | null; error?: Diagnostic } {
    if (this.pending) {
      this.current = this.pending;
      this.pending = null;
    }
    if (!this.current) return { bar: null };
    const result = this.current(n);
    if (result.ok) {
      this.lastGood = copyBar(result.bar);
      return { bar: result.bar };
    }
    return { bar: this.lastGood ? copyBar(this.lastGood) : null, error: result.error };
  }
}
