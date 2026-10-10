import { readdirSync, readFileSync } from "node:fs";
import { resolve } from "node:path";
import { build, type Rollup } from "vite";
import { describe, expect, it } from "vitest";
import {
  EXAMPLE_PATTERNS,
  FENNEL_COMPILER_SOURCE,
  SAMPLE_NAMES,
  sampleUrl,
} from "../../src/player/assets";

const repo = resolve(import.meta.dirname, "../../..");
const wavs = readdirSync(resolve(repo, "sound/808")).filter((f) => f.endsWith(".wav")).sort();

describe("player assets", () => {
  it("lists every bundled 808 sample", () => {
    expect(SAMPLE_NAMES).toEqual(wavs);
    expect(SAMPLE_NAMES).toHaveLength(15);
  });

  it("gives each sample a URL", () => {
    for (const name of SAMPLE_NAMES) expect(sampleUrl(name)).toContain(name.replace(".wav", ""));
    expect(() => sampleUrl("missing.wav")).toThrow(/unknown sample/);
  });

  it("ships the example patterns from patterns/", () => {
    const files = readdirSync(resolve(repo, "patterns")).filter((f) => f.endsWith(".fnl")).sort();
    expect(EXAMPLE_PATTERNS.map((p) => `${p.name}.fnl`)).toEqual(files);
    for (const p of EXAMPLE_PATTERNS) {
      expect(p.source).toBe(readFileSync(resolve(repo, "patterns", `${p.name}.fnl`), "utf8"));
    }
  });

  it("embeds the vendored Fennel compiler", () => {
    expect(FENNEL_COMPILER_SOURCE).toBe(
      readFileSync(resolve(repo, "internal/engine/fennel/compiler.lua"), "utf8"),
    );
  });

  it("emits every sample as a file in the production build", async () => {
    const output = (await build({
      root: resolve(import.meta.dirname, "../.."),
      logLevel: "silent",
      build: { write: false },
    })) as Rollup.RollupOutput | Rollup.RollupOutput[];
    const files = (Array.isArray(output) ? output : [output]).flatMap((o) => o.output.map((f) => f.fileName));
    for (const wav of wavs) {
      expect(files.some((f) => f.endsWith(".wav") && f.includes(wav.replace(".wav", "")))).toBe(true);
    }
  }, 60_000);
});
