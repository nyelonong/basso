import { describe, expect, it } from "vitest";
import { SAMPLE_NAMES, sampleUrl } from "../../src/player/assets";
import { MASTER_GAIN, createWebAudioSink, softClip } from "../../src/player/audio";
import { FakeCompressor, FakeContext, FakeGain, FakePanner, FakeShaper, type FakeNode } from "./fake-audio";

async function setup(voices?: null) {
  const ctx = new FakeContext();
  const fetched: string[] = [];
  const fetchBytes = async (url: string) => {
    fetched.push(url);
    return new ArrayBuffer(8);
  };
  const sink = await createWebAudioSink(ctx.asContext(), fetchBytes, ...(voices === null ? [null] : []));
  return { ctx, sink, fetched };
}

describe("createWebAudioSink", () => {
  it("fetches and decodes every sample once", async () => {
    const { ctx, fetched } = await setup();
    expect(fetched.sort()).toEqual(SAMPLE_NAMES.map(sampleUrl).sort());
    expect(ctx.decoded).toHaveLength(SAMPLE_NAMES.length);
  });

  it("plays a sample at its time with velocity as gain and pan", async () => {
    const { ctx, sink } = await setup();
    sink.playSample("kick2.wav", 3.5, 0.6, -0.4);
    const [source] = ctx.sources();
    expect(source.startedAt).toBe(3.5);
    expect(source.buffer).not.toBeNull();
    const gain = source.connections[0] as FakeGain;
    const panner = gain.connections[0] as FakePanner;
    expect(gain.gain.value).toBe(0.6);
    expect(panner.pan.value).toBe(-0.4);
    const master = panner.connections[0] as FakeGain;
    expect(master.gain.value).toBe(MASTER_GAIN);
  });

  it("routes everything through gain, limiter and soft clipper to the output", async () => {
    const { ctx, sink } = await setup();
    sink.playSample("kick2.wav", 0, 1, 0);
    let node: FakeNode = ctx.sources()[0];
    const chain: string[] = [];
    while (node !== ctx.destination) {
      node = node.connections[0];
      chain.push(node.kind);
    }
    expect(chain).toEqual(["gain", "panner", "gain", "compressor", "shaper", "gain", "destination"]);
    const limiter = ctx.nodes.find((n): n is FakeCompressor => n instanceof FakeCompressor)!;
    expect([limiter.threshold.value, limiter.knee.value, limiter.ratio.value]).toEqual([-6, 0, 20]);
    expect([limiter.attack.value, limiter.release.value]).toEqual([0.001, 0.1]);
    const shaper = ctx.nodes.find((n): n is FakeShaper => n instanceof FakeShaper)!;
    const curve = shaper.curve!;
    expect(shaper.oversample).toBe("4x");
    expect(curve.length % 2).toBe(1);
    expect(Math.abs(curve[(curve.length - 1) / 2])).toBe(0);
    for (let i = 0; i < curve.length; i++) expect(curve[i] + curve[curve.length - 1 - i]).toBe(0);
    expect(Math.max(...curve.map(Math.abs))).toBeLessThan(1);
  });

  it("sends synth notes through the master bus too", async () => {
    const { ctx, sink } = await setup();
    sink.playNote("C2", "bass", 0, 1, 1, 0);
    const master = ctx.nodes.find((n) => n.kind === "gain")!;
    expect(ctx.nodes.some((n) => n.kind === "panner" && n.connections[0] === master)).toBe(true);
  });

  it("stops every active source", async () => {
    const { ctx, sink } = await setup();
    sink.playSample("kick2.wav", 1, 1, 0);
    sink.playSample("snare.wav", 2, 1, 0);
    const [ended, active] = ctx.sources();
    ended.onended?.();
    sink.stopAll();
    expect(ended.stoppedAt).toBeNull();
    expect(active.stoppedAt).not.toBeNull();
  });

  it("plays notes through the built-in synth by default", async () => {
    const { ctx, sink } = await setup();
    sink.playNote("C2", "bass", 1, 0.5, 1, 0);
    expect(ctx.sources()).toHaveLength(1);
    expect(ctx.sources()[0].startedAt).toBe(1);
  });

  it("ignores notes when voices are turned off", async () => {
    const { ctx, sink } = await setup(null);
    expect(() => sink.playNote("C2", "bass", 0, 1, 1, 0)).not.toThrow();
    expect(ctx.sources()).toHaveLength(0);
  });

  it("tracks voice sources so stopAll silences them", async () => {
    const { ctx, sink } = await setup();
    sink.voices = {
      playNote: (c, destination) => {
        const osc = c.createOscillator();
        osc.connect(destination);
        osc.start(0);
        return [osc];
      },
    };
    sink.playNote("C2", "bass", 0, 1, 1, 0);
    sink.stopAll();
    expect(ctx.sources()[0].stoppedAt).not.toBeNull();
  });
});

describe("softClip", () => {
  it("leaves quiet signal alone", () => {
    for (const x of [-0.8, -0.3, 0, 0.5, 0.8]) expect(softClip(x)).toBe(x);
  });

  it("stays below full scale and keeps rising up to it", () => {
    expect(softClip(0.9)).toBeGreaterThan(softClip(0.85));
    expect(softClip(1)).toBeGreaterThan(softClip(0.9));
    expect(softClip(1)).toBeLessThan(0.96);
    expect(softClip(-1)).toBe(-softClip(1));
  });
});
