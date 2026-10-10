import { describe, expect, it } from "vitest";
import type { Instrument } from "../../src/player/model";
import { noteFrequency } from "../../src/player/notes";
import { envelopeTimes, synthVoices } from "../../src/player/synth";
import { FakeBuffer, FakeContext, FakeGain, FakeIIR, FakeNode, FakePanner, FakeSource } from "./fake-audio";

function play(instrument: Instrument, note = "A3", time = 2, duration = 0.5, velocity = 0.8, pan = -0.3) {
  const ctx = new FakeContext();
  const sources = synthVoices.playNote(
    ctx.asContext(),
    ctx.destination as unknown as AudioNode,
    note,
    instrument,
    time,
    duration,
    velocity,
    pan,
  ) as unknown as FakeSource[];
  const kinds = (kind: string) => ctx.nodes.filter((n) => n.kind === kind);
  const panner = kinds("panner")[0] as FakePanner;
  const envelope = kinds("gain").find((g) => g.connections.includes(panner)) as FakeGain;
  return { ctx, sources, kinds, panner, envelope };
}

const freqs = (sources: FakeSource[]) => sources.map((s) => s.frequency.events[0]?.[1] ?? s.frequency.value);

describe("envelopeTimes", () => {
  it.each([
    [1, 0.008, 0.03],
    [0.1, 0.008, 0.02],
    [0.01, 0.008, 0.002],
    [0.005, 0.004, 0.001],
  ])("splits a %f s note like the Go engine", (total, attack, release) => {
    const times = envelopeTimes(total, 0.008, 0.03, 0.2);
    expect(times.attack).toBeCloseTo(attack, 9);
    expect(times.release).toBeCloseTo(release, 9);
  });
});

describe("synthVoices", () => {
  it.each([
    ["bass", ["sawtooth"]],
    ["brass", ["sawtooth"]],
    ["lead", ["square", "sawtooth"]],
  ] as const)("builds %s from %j oscillators at the note frequency", (instrument, types) => {
    const { sources } = play(instrument);
    expect(sources.map((s) => s.type)).toEqual(types);
    for (const f of freqs(sources)) expect(f).toBeCloseTo(noteFrequency("A3"), 6);
  });

  it("detunes the pad's saws by 7 cents around a triangle", () => {
    const { sources } = play("pad");
    expect(sources.map((s) => s.type)).toEqual(["sawtooth", "triangle", "sawtooth"]);
    const f = noteFrequency("A3");
    const [low, mid, high] = freqs(sources);
    expect(low).toBeCloseTo(f * 2 ** (-7 / 1200), 6);
    expect(mid).toBeCloseTo(f, 6);
    expect(high).toBeCloseTo(f * 2 ** (7 / 1200), 6);
  });

  it.each(["bass", "brass", "lead", "pad", "pluck"] as const)("starts and stops %s on time", (instrument) => {
    const { sources } = play(instrument);
    expect(sources.length).toBeGreaterThan(0);
    for (const s of sources) {
      expect(s.startedAt).toBe(2);
      expect(s.stoppedAt).toBeCloseTo(2.5, 9);
    }
  });

  it.each(["bass", "brass", "lead", "pad", "pluck"] as const)("shapes %s with velocity and pan", (instrument) => {
    const { envelope, panner, ctx } = play(instrument);
    expect(Math.max(...envelope.gain.events.map((e) => e[1]))).toBeCloseTo(0.8, 9);
    expect(envelope.gain.events.at(-1)).toEqual(["linear", 0, expect.closeTo(2.5, 9)]);
    expect(panner.pan.value).toBe(-0.3);
    expect(panner.connections).toContain(ctx.destination);
  });

  it("ramps the bass attack over 8 ms and the brass over 40 ms", () => {
    const attackEnd = (instrument: Instrument) => play(instrument).envelope.gain.events[1];
    expect(attackEnd("bass")).toEqual(["linear", 0.8, expect.closeTo(2.008, 9)]);
    expect(attackEnd("brass")).toEqual(["linear", 0.8, expect.closeTo(2.04, 9)]);
  });

  it("filters lead and pad through Go's one-pole low-pass, not bass", () => {
    const alpha = (cutoff: number) => 1 - Math.exp((-2 * Math.PI * cutoff) / 44100);
    const lead = play("lead").kinds("iir")[0] as FakeIIR;
    expect(lead.feedforward[0]).toBeCloseTo(alpha(6000), 9);
    expect(lead.feedback).toEqual([1, -(1 - lead.feedforward[0])]);
    const pad = play("pad").kinds("iir")[0] as FakeIIR;
    expect(pad.feedforward[0]).toBeCloseTo(alpha(1800), 9);
    expect(play("bass").kinds("iir")).toHaveLength(0);
  });

  it("mixes lead and pad oscillators with Go's weights", () => {
    const weights = (instrument: Instrument) =>
      play(instrument)
        .sources.map((s) => (s.connections[0] as FakeGain).gain.value);
    expect(weights("lead")).toEqual([0.55, 0.35]);
    expect(weights("pad")).toEqual([0.25, 0.3, 0.25]);
  });

  it("renders the pluck as a decaying string buffer of the note's length", () => {
    const { sources } = play("pluck", "A3", 2, 0.5);
    expect(sources).toHaveLength(1);
    const buffer = sources[0].buffer as FakeBuffer;
    expect(buffer.length).toBe(Math.round(0.5 * 44100));
    const head = Math.max(...buffer.data.slice(0, 500).map(Math.abs));
    const tail = Math.max(...buffer.data.slice(-500).map(Math.abs));
    expect(head).toBeGreaterThan(0);
    expect(tail).toBeLessThan(head);
  });

  it("has no envelope attack on the pluck", () => {
    const events = play("pluck").envelope.gain.events;
    expect(events[0]).toEqual(["set", 0.8, 2]);
  });

  it("connects every node into the graph", () => {
    const { ctx } = play("pad");
    const reachable = new Set<FakeNode>();
    const visit = (n: FakeNode) => {
      if (reachable.has(n)) return;
      reachable.add(n);
      n.connections.forEach(visit);
    };
    ctx.sources().forEach(visit);
    for (const n of ctx.nodes) expect(reachable.has(n)).toBe(true);
    expect(reachable.has(ctx.destination)).toBe(true);
  });
});
