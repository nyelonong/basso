import { describe, expect, it } from "vitest";
import type { AudioSink, Bar, BarProvider, Hit } from "../../src/player/model";
import { encodeWav, renderPattern, type RenderContext, type RenderedAudio } from "../../src/player/render";

type Call = { kind: string; time: number; duration?: number };

function harness() {
  const calls: Call[] = [];
  const created: { channels: number; frames: number; sampleRate: number }[] = [];
  const sink: AudioSink = {
    playSample: (_name, time) => calls.push({ kind: "sample", time }),
    playNote: (_note, _instrument, time, duration) => calls.push({ kind: "note", time, duration }),
    stopAll: () => {},
  };
  const deps = {
    createContext(channels: number, frames: number, sampleRate: number) {
      created.push({ channels, frames, sampleRate });
      const audio: RenderedAudio = {
        numberOfChannels: channels,
        sampleRate,
        length: frames,
        getChannelData: () => new Float32Array(frames),
      };
      return { startRendering: async () => audio } as unknown as RenderContext;
    },
    fetchBytes: async () => new ArrayBuffer(0),
    createSink: async () => sink,
    measureDelay: async () => 400,
  };
  return { calls, created, deps };
}

const kick = (step: number): Hit => ({ kind: "sample", sample: "kick2.wav", step, velocity: 1, pan: 0 });
const bars =
  (...list: Bar[]): BarProvider =>
  (n) => ({ ok: true, bar: list[n % list.length] });

describe("renderPattern", () => {
  it("renders exactly the requested bars on one continuous clock", async () => {
    const h = harness();
    const provider = bars(
      { bpm: 120, steps: 16, swing: 50, hits: [kick(0)] },
      { bpm: 60, steps: 4, swing: 75, hits: [kick(1)] },
    );
    const result = await renderPattern(provider, 2, h.deps);
    // 16 steps at 0.125 s, then 4 steps at 0.25 s.
    expect(result).toMatchObject({ ok: true, seconds: 3 });
    // A 0.1 s lead-in (4410 frames) and the 400-frame bus delay, both trimmed from the front of the file.
    expect(h.created).toEqual([{ channels: 2, frames: 4410 + 400 + 3 * 44100, sampleRate: 44100 }]);
    if (result.ok) expect(result.wav.length).toBe(44 + 3 * 44100 * 4);
    expect(h.calls.map((c) => c.time)).toEqual([0.1, 0.1 + 2 + 1.5 * 0.25]);
  });

  it("refuses renders longer than five minutes", async () => {
    const h = harness();
    // 256 steps at 20 BPM is 192 seconds per bar.
    const result = await renderPattern(bars({ bpm: 20, steps: 256, swing: 50, hits: [] }), 2, h.deps);
    expect(result).toEqual({ ok: false, error: { message: "2 bars last 384 seconds; renders are limited to 300" } });
    expect(h.created).toEqual([]);
  });

  it("reports the first failing bar instead of rendering", async () => {
    const h = harness();
    const provider: BarProvider = (n) =>
      n < 2 ? { ok: true, bar: { bpm: 120, steps: 16, swing: 50, hits: [] } } : { ok: false, error: { message: "boom", bar: n } };
    expect(await renderPattern(provider, 4, h.deps)).toEqual({ ok: false, error: { message: "boom", bar: 2 } });
    expect(h.created).toEqual([]);
  });
});

describe("encodeWav", () => {
  const audio = (left: number[], right: number[]): RenderedAudio => ({
    numberOfChannels: 2,
    sampleRate: 44100,
    length: left.length,
    getChannelData: (c) => Float32Array.from(c === 0 ? left : right),
  });

  it("writes a 16-bit stereo PCM header", () => {
    const wav = encodeWav(audio([0, 0], [0, 0]));
    const view = new DataView(wav.buffer);
    const text = (at: number) => String.fromCharCode(...wav.slice(at, at + 4));
    expect([text(0), text(8), text(12), text(36)]).toEqual(["RIFF", "WAVE", "fmt ", "data"]);
    expect(view.getUint32(4, true)).toBe(wav.length - 8);
    expect(view.getUint16(20, true)).toBe(1);
    expect(view.getUint16(22, true)).toBe(2);
    expect(view.getUint32(24, true)).toBe(44100);
    expect(view.getUint16(34, true)).toBe(16);
    expect(view.getUint32(40, true)).toBe(8);
  });

  it("drops the frames before startFrame", () => {
    const view = new DataView(encodeWav(audio([0.1, 0.5], [0.2, -0.5]), 1).buffer);
    expect(view.getUint32(40, true)).toBe(4);
    expect([view.getInt16(44, true), view.getInt16(46, true)]).toEqual([16384, -16383]);
  });

  it("interleaves channels and clips samples past full scale", () => {
    const view = new DataView(encodeWav(audio([0.5, 2], [-0.5, -2])).buffer);
    expect([0, 1, 2, 3].map((i) => view.getInt16(44 + i * 2, true))).toEqual([16384, -16383, 32767, -32767]);
  });
});
