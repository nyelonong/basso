import { watch } from "node:fs";
import { readFile, writeFile } from "node:fs/promises";
import { basename, dirname, resolve } from "node:path";
import { AudioContext, OfflineAudioContext } from "node-web-audio-api";
import { createWebAudioSink } from "../player/audio";
import { createFennelRuntime } from "../player/fennel";
import { FENNEL_COMPILER_SOURCE } from "../player/fennel-loader";
import type { RenderContext } from "../player/render";
import { runCli } from "./main";

const SOUND_DIR = resolve(import.meta.dirname, "../../../sound/808");

// Sample URLs from the bundler point at sound/808; the CLI reads the same files from disk.
async function fetchBytes(url: string): Promise<ArrayBuffer> {
  const bytes = await readFile(resolve(SOUND_DIR, basename(url.split("?")[0])));
  return bytes.buffer.slice(bytes.byteOffset, bytes.byteOffset + bytes.byteLength) as ArrayBuffer;
}

export function main(args: string[], cwd: string): Promise<number> {
  return runCli(args, {
    cwd,
    resolvePath: (from, file) => resolve(from, file),
    runtime: () => createFennelRuntime(FENNEL_COMPILER_SOURCE),
    readText: (path) => readFile(path, "utf8"),
    writeBytes: (path, bytes) => writeFile(path, bytes),
    out: (line) => process.stdout.write(`${line}\n`),
    err: (line) => process.stderr.write(`${line}\n`),
    render: {
      createContext: (channels, frames, sampleRate) =>
        new OfflineAudioContext(channels, frames, sampleRate) as unknown as RenderContext,
      fetchBytes,
    },
    play: {
      async createSink() {
        const ctx = new AudioContext({ latencyHint: "playback" });
        const sink = await createWebAudioSink(ctx as unknown as BaseAudioContext, fetchBytes);
        return { sink, now: () => ctx.currentTime, close: () => ctx.close() };
      },
      watch(path, onChange) {
        let timer: ReturnType<typeof setTimeout> | undefined;
        // Many editors save by writing a new file and renaming it over the old
        // one, and a watcher on the file itself stops at the first rename. The
        // folder is watched instead, by name. Saves often arrive in several
        // events; wait for the last one.
        const name = basename(path);
        const watcher = watch(dirname(path), (_event, changed) => {
          if (changed !== null && changed !== name) return;
          clearTimeout(timer);
          timer = setTimeout(onChange, 100);
        });
        return () => {
          clearTimeout(timer);
          watcher.close();
        };
      },
      untilStopped: () => new Promise((done) => process.once("SIGINT", () => done())),
    },
  });
}
