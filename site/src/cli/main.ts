import { analyzeAudio, type Analysis } from "../player/analyze";
import { SAMPLE_NAMES } from "../player/assets";
import { fennelProvider } from "../player/evaluate";
import type { FennelRuntime } from "../player/fennel";
import { INSTRUMENTS, type AudioSink, type Bar, type Diagnostic } from "../player/model";
import { encodeWav, renderAudio, type RenderDeps } from "../player/render";
import { Session } from "../player/session";

export type CliIO = {
  cwd: string;
  resolvePath(cwd: string, file: string): string;
  runtime: () => Promise<FennelRuntime>;
  readText(path: string): Promise<string>;
  writeBytes(path: string, bytes: Uint8Array): Promise<void>;
  out(line: string): void;
  err(line: string): void;
  render: RenderDeps;
  play?: PlayIO;
};

export type PlayIO = {
  createSink(): Promise<{ sink: AudioSink; now(): number; close(): Promise<void> }>;
  watch(path: string, onChange: () => void): () => void;
  untilStopped(): Promise<void>;
};

const USAGE = `usage: basso <command> [options]

commands:
  check <file.fnl> [--bars N] [--json]               evaluate N bars (default 16) and report errors
  render <file.fnl> [--bars N] [--out F.wav] [--json] render N bars (default 8), measure them, optionally save a WAV
  play <file.fnl>                                    play live; saving the file applies changes at the next bar
  samples [--json]                                   list sample names and synth instruments`;

type Options = { file?: string; bars?: number; out?: string; json: boolean };

function parse(args: string[]): Options | string {
  const options: Options = { json: false };
  for (let i = 0; i < args.length; i++) {
    const arg = args[i];
    if (arg === "--json") options.json = true;
    else if (arg === "--bars") {
      const value = Number(args[++i]);
      if (!Number.isInteger(value) || value < 1) return "--bars needs a whole number of at least 1";
      options.bars = value;
    } else if (arg === "--out") {
      const value = args[++i];
      if (!value) return "--out needs a file name";
      options.out = value;
    } else if (arg.startsWith("--")) return `unknown option ${arg}`;
    else if (options.file === undefined) options.file = arg;
    else return `unexpected argument ${arg}`;
  }
  return options;
}

// Pans in check and render come from a fixed seed so the same file measures the same way twice.
function seededRandom(seed = 1): () => number {
  let state = seed;
  return () => {
    state = (state + 0x6d2b79f5) | 0;
    let t = Math.imul(state ^ (state >>> 15), 1 | state);
    t = (t + Math.imul(t ^ (t >>> 7), 61 | t)) ^ t;
    return ((t ^ (t >>> 14)) >>> 0) / 4294967296;
  };
}

const where = (file: string, d: Diagnostic) =>
  `${file}${d.line === undefined ? "" : `:${d.line}`}${d.bar === undefined ? "" : ` (bar ${d.bar})`}: ${d.message}`;

export async function runCli(args: string[], io: CliIO): Promise<number> {
  const [command, ...rest] = args;
  if (command === undefined || command === "help" || command === "--help") {
    io.out(USAGE);
    return command === undefined ? 1 : 0;
  }
  const options = parse(rest);
  if (typeof options === "string") {
    io.err(`${options}\n\n${USAGE}`);
    return 2;
  }

  if (command === "samples") {
    if (options.json) io.out(JSON.stringify({ samples: SAMPLE_NAMES, instruments: INSTRUMENTS }, null, 2));
    else io.out(`samples:\n  ${SAMPLE_NAMES.join("\n  ")}\ninstruments (for :note hits):\n  ${INSTRUMENTS.join("\n  ")}`);
    return 0;
  }
  if (command !== "check" && command !== "render" && command !== "play") {
    io.err(`unknown command ${command}\n\n${USAGE}`);
    return 2;
  }
  if (options.file === undefined) {
    io.err(`${command} needs a pattern file\n\n${USAGE}`);
    return 2;
  }

  const path = io.resolvePath(io.cwd, options.file);
  let source: string;
  try {
    source = await io.readText(path);
  } catch {
    io.err(`cannot read ${options.file}`);
    return 1;
  }
  const runtime = await io.runtime();
  if (command === "play") return play(options.file, path, source, runtime, io);

  const compiled = runtime.compile(source);
  if (!compiled.ok) return fail(io, options, options.file, compiled.error);
  const provider = fennelProvider(runtime, compiled.pattern, SAMPLE_NAMES, seededRandom());

  if (command === "check") {
    const count = options.bars ?? 16;
    const bars: Bar[] = [];
    for (let n = 0; n < count; n++) {
      const result = provider(n);
      if (!result.ok) return fail(io, options, options.file, result.error);
      bars.push(result.bar);
    }
    const summary = {
      ok: true,
      bars: count,
      bpm: [...new Set(bars.map((b) => b.bpm))],
      steps: [...new Set(bars.map((b) => b.steps))],
      swing: [...new Set(bars.map((b) => b.swing))],
      hits: bars.reduce((total, b) => total + b.hits.length, 0),
      samples: [...new Set(bars.flatMap((b) => b.hits.flatMap((h) => (h.kind === "sample" ? [h.sample] : []))))].sort(),
      instruments: [...new Set(bars.flatMap((b) => b.hits.flatMap((h) => (h.kind === "note" ? [h.instrument] : []))))].sort(),
    };
    if (options.json) io.out(JSON.stringify(summary, null, 2));
    else {
      io.out(`ok: ${count} bars, ${summary.hits} hits, bpm ${summary.bpm.join("/")}, steps ${summary.steps.join("/")}, swing ${summary.swing.join("/")}`);
      io.out(`sounds: ${[...summary.samples, ...summary.instruments].join(", ")}`);
    }
    return 0;
  }

  const result = await renderAudio(provider, options.bars ?? 8, io.render);
  if (!result.ok) return fail(io, options, options.file, result.error);
  const analysis = analyzeAudio(result.audio, result.bars);
  if (options.out) {
    try {
      await io.writeBytes(io.resolvePath(io.cwd, options.out), encodeWav(result.audio));
    } catch (error) {
      return fail(io, options, options.file, { message: `cannot write ${options.out}: ${(error as Error).message}` });
    }
  }
  if (options.json) io.out(JSON.stringify({ ok: true, out: options.out ?? null, ...analysis }, null, 2));
  else io.out(report(analysis, result.bars.length, options.out));
  return 0;
}

function fail(io: CliIO, options: Options, file: string, error: Diagnostic): number {
  if (options.json) io.out(JSON.stringify({ ok: false, error }, null, 2));
  else io.err(`error: ${where(file, error)}`);
  return 1;
}

function report(a: Analysis, bars: number, out: string | undefined): string {
  const pad = (value: number | string, width = 7) => String(value).padStart(width);
  const lines = [
    `rendered ${bars} bars, ${a.seconds.toFixed(1)} s${out ? ` -> ${out}` : ""}`,
    `peak ${a.peakDb} dB  rms ${a.rmsDb} dB  clipped ${a.clippedSamples} samples  limited ${a.limitedPercent}%`,
    `bands  low ${a.bands.lowDb} dB  mid ${a.bands.midDb} dB  high ${a.bands.highDb} dB  (dB, under 200 Hz / 200 Hz-4 kHz / over 4 kHz)`,
    `${pad("bar", 4)}${pad("rms")}${pad("low")}${pad("mid")}${pad("high")}`,
    ...a.bars.map((b) => `${pad(b.bar, 4)}${pad(b.rmsDb)}${pad(b.lowDb)}${pad(b.midDb)}${pad(b.highDb)}`),
  ];
  lines.push(a.warnings.length === 0 ? "warnings: none" : `warnings:\n  - ${a.warnings.join("\n  - ")}`);
  return lines.join("\n");
}

async function play(file: string, path: string, source: string, runtime: FennelRuntime, io: CliIO): Promise<number> {
  if (!io.play) {
    io.err("play is not available here");
    return 1;
  }
  const playIO = io.play;
  const audio = await playIO.createSink();
  let lastBpm = 0;
  let playing = false;
  // Session reports a failing bar every time it comes round, such as a fill bar
  // every fourth bar; print each problem once per saved version of the file.
  const shownErrors = new Set<string>();
  const session = new Session(
    {
      runtime,
      sink: audio.sink,
      clock: { now: audio.now },
      timer: {
        every: (ms, fn) => {
          const id = setInterval(fn, ms);
          return () => clearInterval(id);
        },
      },
      sampleNames: SAMPLE_NAMES,
      random: Math.random,
    },
    {
      onBar: (n, bar) => {
        if (bar && bar.bpm !== lastBpm) io.out(`bar ${n}: ${bar.bpm} BPM`);
        lastBpm = bar?.bpm ?? lastBpm;
      },
      onDiagnostic: (d) => {
        if (d === null || shownErrors.has(d.message)) return;
        shownErrors.add(d.message);
        io.err(`error: ${where(file, d)}${playing ? " (still playing the last good pattern)" : ""}`);
      },
      onState: () => {},
    },
  );

  const first = session.updateCode(source);
  if (first) {
    await audio.close();
    return 1;
  }
  io.out(`playing ${file}. Save the file to apply changes at the next bar. Ctrl+C stops.`);
  playing = true;
  session.play();
  let latestRead = 0;
  let missing = false;
  const stopWatching = playIO.watch(path, async () => {
    const read = ++latestRead;
    const next = await io.readText(path).catch(() => null);
    // A slower read of an older save must not replace a newer one.
    if (read !== latestRead) return;
    if (next === null) {
      if (!missing) io.err(`cannot read ${file}; still playing the last good pattern until it is back`);
      missing = true;
      return;
    }
    missing = false;
    if (next === source) return;
    source = next;
    shownErrors.clear();
    if (session.updateCode(next) === null) io.out("update queued for the next bar");
  });
  await playIO.untilStopped();
  stopWatching();
  session.stop();
  await audio.close();
  return 0;
}
