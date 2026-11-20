// The dust hears the beat from the scheduler's own hit times, so it stays exact without analysing the audio.
export const KICK = 0;
export const SNARE = 1;
export const HAT = 2;
export const TONE = 3;
export type Voice = typeof KICK | typeof SNARE | typeof HAT | typeof TONE;

const DECAY: readonly number[] = [0.22, 0.15, 0.07, 0.18];

export function voiceOf(sample: string): Voice {
  const name = sample.toLowerCase();
  if (name.startsWith("kick")) return KICK;
  if (/snare|clap|rim/.test(name)) return SNARE;
  if (/hh|hihat|cym|maracas|claves|cowbell/.test(name)) return HAT;
  return TONE;
}

type Pending = { time: number; voice: Voice; velocity: number };

export class BeatQueue {
  private pending: Pending[] = [];
  private readonly last = [-Infinity, -Infinity, -Infinity, -Infinity];
  private readonly lastVelocity = [0, 0, 0, 0];

  add(voice: Voice, time: number, velocity: number): void {
    const hit = { time, voice, velocity };
    let i = this.pending.length;
    while (i > 0 && this.pending[i - 1].time > time) i--;
    this.pending.splice(i, 0, hit);
  }

  clear(): void {
    this.pending = [];
    this.last.fill(-Infinity);
    this.lastVelocity.fill(0);
  }

  /** Envelope per voice at `now` (0 to about 1), after taking every hit due by then. */
  envelopes(now: number): [number, number, number, number] {
    let done = 0;
    while (done < this.pending.length && this.pending[done].time <= now) {
      const hit = this.pending[done++];
      const same = this.last[hit.voice] === hit.time;
      this.last[hit.voice] = hit.time;
      this.lastVelocity[hit.voice] = same ? Math.max(this.lastVelocity[hit.voice], hit.velocity) : hit.velocity;
    }
    if (done) this.pending.splice(0, done);
    const level = (v: number) => (this.last[v] === -Infinity ? 0 : this.lastVelocity[v] * Math.exp(-(now - this.last[v]) / DECAY[v]));
    return [level(0), level(1), level(2), level(3)];
  }

  /** Seconds since the last kick at `now`, or Infinity when none has played. */
  kickAge(now: number): number {
    return now - this.last[KICK];
  }
}
