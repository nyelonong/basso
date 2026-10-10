import { describe, expect, it } from "vitest";
import { SAMPLE_NAMES, sampleUrl } from "../../src/player/assets";
import { createWebAudioSink } from "../../src/player/audio";
import { FakeContext, FakeGain, FakePanner } from "./fake-audio";

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
    expect(panner.connections[0]).toBe(ctx.destination);
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
