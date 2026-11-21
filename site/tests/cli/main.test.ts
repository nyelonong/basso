import { readFileSync } from "node:fs";
import { resolve, posix } from "node:path";
import { describe, expect, it } from "vitest";
import { runCli, type CliIO, type PlayIO } from "../../src/cli/main";
import { SAMPLE_NAMES } from "../../src/player/assets";
import { createFennelRuntime } from "../../src/player/fennel";
import type { AudioSink } from "../../src/player/model";
import type { RenderContext, RenderedAudio } from "../../src/player/render";

const repo = resolve(import.meta.dirname, "../../..");
const runtime = createFennelRuntime(readFileSync(resolve(repo, "internal/engine/fennel/compiler.lua"), "utf8"));

const silentSink: AudioSink = { playSample: () => {}, playNote: () => {}, stopAll: () => {} };

function harness(files: Record<string, string>, play?: PlayIO) {
  const out: string[] = [];
  const err: string[] = [];
  const written: Record<string, Uint8Array> = {};
  const io: CliIO = {
    cwd: "/work",
    resolvePath: (from, file) => posix.resolve(from, file),
    runtime: () => runtime,
    readText: async (path) => {
      if (!(path in files)) throw new Error("missing");
      return files[path];
    },
    writeBytes: async (path, bytes) => {
      if (path.startsWith("/missing/")) throw new Error("ENOENT: no such file or directory");
      written[path] = bytes;
    },
    out: (line) => out.push(line),
    err: (line) => err.push(line),
    render: {
      createContext: (channels, frames, sampleRate) => {
        const audio: RenderedAudio = {
          numberOfChannels: channels,
          sampleRate,
          length: frames,
          getChannelData: () => Float32Array.from({ length: frames }, (_, i) => 0.5 * Math.sin(i / 3)),
        };
        return { startRendering: async () => audio } as unknown as RenderContext;
      },
      fetchBytes: async () => new ArrayBuffer(0),
      createSink: async () => silentSink,
      measureDelay: async () => 0,
    },
    play,
  };
  return { io, out, err, written };
}

const GOOD = '(bpm 128)\n(fn pattern [bar]\n  (seq "kick2.wav" "x... x... x... x..."))\npattern\n';
const BAD = '(fn pattern [bar]\n  [{:step 0 :sample "nope.wav"}])\npattern\n';

describe("basso cli", () => {
  it("lists samples and instruments", async () => {
    const h = harness({});
    expect(await runCli(["samples", "--json"], h.io)).toBe(0);
    expect(JSON.parse(h.out[0])).toEqual({ samples: SAMPLE_NAMES, instruments: ["bass", "brass", "pluck", "lead", "pad"] });
  });

  it("checks a good pattern and summarises it", async () => {
    const h = harness({ "/work/beat.fnl": GOOD });
    expect(await runCli(["check", "beat.fnl", "--bars", "4", "--json"], h.io)).toBe(0);
    expect(JSON.parse(h.out[0])).toMatchObject({ ok: true, bars: 4, bpm: [128], hits: 16, samples: ["kick2.wav"] });
  });

  it("reports a broken pattern with the bar and exits 1", async () => {
    const h = harness({ "/work/bad.fnl": BAD });
    expect(await runCli(["check", "bad.fnl"], h.io)).toBe(1);
    expect(h.err[0]).toBe('error: bad.fnl (bar 0): hit 0 sample "nope.wav" is not in the sound inventory');
  });

  it("reports a compile error with its line", async () => {
    const h = harness({ "/work/typo.fnl": "(fn pattern [bar]\n  (seq \"kick2.wav\" \"x\")\n" });
    expect(await runCli(["check", "typo.fnl", "--json"], h.io)).toBe(1);
    expect(JSON.parse(h.out[0])).toMatchObject({ ok: false, error: { line: expect.any(Number) } });
  });

  it("renders, measures and writes a WAV", async () => {
    const h = harness({ "/work/beat.fnl": GOOD });
    expect(await runCli(["render", "beat.fnl", "--bars", "2", "--out", "beat.wav", "--json"], h.io)).toBe(0);
    const result = JSON.parse(h.out[0]);
    expect(result).toMatchObject({ ok: true, out: "beat.wav", peakDb: expect.any(Number), warnings: expect.any(Array) });
    expect(result.bars).toHaveLength(2);
    const wav = h.written["/work/beat.wav"];
    expect(String.fromCharCode(...wav.slice(0, 4))).toBe("RIFF");
  });

  it("reports a failed WAV write as JSON instead of crashing", async () => {
    const h = harness({ "/work/beat.fnl": GOOD });
    expect(await runCli(["render", "beat.fnl", "--bars", "1", "--out", "/missing/x.wav", "--json"], h.io)).toBe(1);
    expect(JSON.parse(h.out[0])).toEqual({
      ok: false,
      error: { message: "cannot write /missing/x.wav: ENOENT: no such file or directory" },
    });
  });

  it("refuses unknown commands and options", async () => {
    expect(await runCli(["dance"], harness({}).io)).toBe(2);
    expect(await runCli(["check", "x.fnl", "--loud"], harness({}).io)).toBe(2);
    expect(await runCli(["check", "x.fnl", "--bars", "0"], harness({}).io)).toBe(2);
  });

  it("plays, applies a saved change and keeps playing through a broken save", async () => {
    const files = { "/work/live.fnl": GOOD };
    let changed: () => void = () => {};
    let stop: () => void = () => {};
    const stopped = new Promise<void>((done) => (stop = done));
    let closed = false;
    const h = harness(files, {
      createSink: async () => ({ sink: silentSink, now: () => 0, close: async () => void (closed = true) }),
      watch: (_path, onChange) => {
        changed = onChange;
        return () => {};
      },
      untilStopped: () => stopped,
    });
    const running = runCli(["play", "live.fnl"], h.io);
    await new Promise((r) => setTimeout(r, 10));
    files["/work/live.fnl"] = GOOD.replace("128", "140");
    changed();
    await new Promise((r) => setTimeout(r, 10));
    files["/work/live.fnl"] = BAD;
    changed();
    await new Promise((r) => setTimeout(r, 10));
    stop();
    expect(await running).toBe(0);
    expect(closed).toBe(true);
    expect(h.out).toContain("update queued for the next bar");
    expect(h.err.join("\n")).toMatch(/nope\.wav.*still playing the last good pattern/);
  });

  it("does not claim to keep playing when the first load fails", async () => {
    const h = harness({ "/work/bad.fnl": BAD }, {
      createSink: async () => ({ sink: silentSink, now: () => 0, close: async () => {} }),
      watch: () => () => {},
      untilStopped: () => new Promise(() => {}),
    });
    expect(await runCli(["play", "bad.fnl"], h.io)).toBe(1);
    expect(h.err).toEqual(['error: bad.fnl (bar 0): hit 0 sample "nope.wav" is not in the sound inventory']);
  });

  it("prints an error that repeats on fill bars once per save, and survives a missing file", async () => {
    const FILL_BREAKS = '(fn pattern [bar]\n  (if (= (% bar 2) 0) [{:step 0 :sample "nope.wav"}] (seq "kick2.wav" "x")))\npattern\n';
    const files: Record<string, string> = { "/work/live.fnl": GOOD };
    let changed: () => void = () => {};
    let stop: () => void = () => {};
    let now = 0;
    const h = harness(files, {
      createSink: async () => ({ sink: silentSink, now: () => now, close: async () => {} }),
      watch: (_path, onChange) => {
        changed = onChange;
        return () => {};
      },
      untilStopped: () => new Promise<void>((done) => (stop = done)),
    });
    const tick = () => new Promise((r) => setTimeout(r, 40));
    const running = runCli(["play", "live.fnl"], h.io);
    await tick();
    files["/work/live.fnl"] = FILL_BREAKS;
    changed();
    // Let several bars pass: the even ones fail, the odd ones play.
    for (let i = 0; i < 6; i++) {
      now += 2;
      await tick();
    }
    delete files["/work/live.fnl"];
    changed();
    await tick();
    changed();
    await tick();
    stop();
    expect(await running).toBe(0);
    const errors = h.err.filter((line) => line.includes("nope.wav"));
    expect(errors).toHaveLength(1);
    expect(h.err.filter((line) => line.startsWith("cannot read"))).toEqual([
      "cannot read live.fnl; still playing the last good pattern until it is back",
    ]);
  });
});
