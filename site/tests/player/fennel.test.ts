import { readFileSync } from "node:fs";
import { resolve } from "node:path";
import { describe, expect, it } from "vitest";
import { createFennelRuntime } from "../../src/player/fennel";

const repo = resolve(import.meta.dirname, "../../..");
const compilerSource = readFileSync(resolve(repo, "internal/engine/fennel/compiler.lua"), "utf8");
const pattern = (name: string) => readFileSync(resolve(repo, "patterns", name), "utf8");

describe("Fennel runtime", () => {
  it("compiles and runs four-on-the-floor in the browser Lua runtime", async () => {
    const runtime = await createFennelRuntime(compilerSource);
    const compiled = runtime.compile(pattern("four-on-the-floor.fnl"));
    expect(compiled.ok).toBe(true);
    if (!compiled.ok) return;
    const result = runtime.run(compiled.pattern, 0);
    expect(result).toMatchObject({ ok: true, bpm: 128, steps: 16 });
    if (result.ok) expect(result.rawHits).toHaveLength(10);
    runtime.close();
  });

  const runtime = createFennelRuntime(compilerSource);

  async function runSource(source: string, bar = 0) {
    const rt = await runtime;
    const compiled = rt.compile(source);
    if (!compiled.ok) return compiled;
    return rt.run(compiled.pattern, bar);
  }

  it.each([
    "basic-groove.fnl",
    "bass-groove.fnl",
    "electronic-palette.fnl",
    "four-on-the-floor.fnl",
    "funk-bass-groove.fnl",
    "generative.fnl",
    "indo-bounce.fnl",
  ])("runs bars 0-3 of %s", async (name) => {
    for (let bar = 0; bar < 4; bar++) {
      const result = await runSource(pattern(name), bar);
      expect(result.ok, JSON.stringify(result)).toBe(true);
      if (result.ok && "rawHits" in result) expect(result.rawHits.length).toBeGreaterThan(0);
    }
  });

  it("defaults bpm to 120 and steps to 16", async () => {
    const result = await runSource("(fn pattern [bar] [])");
    expect(result).toMatchObject({ ok: true, bpm: 120, steps: 16, rawHits: [] });
  });

  it("passes the bar number to the pattern", async () => {
    const source = '(fn pattern [bar] [{:step bar :sample "kick2.wav"}])';
    const result = await runSource(source, 3);
    expect(result).toMatchObject({ ok: true, rawHits: [{ step: 3, sample: "kick2.wav" }] });
  });

  it("does not carry state between bars", async () => {
    const rt = await runtime;
    const compiled = rt.compile(
      "(fn pattern [bar] (when (= bar 0) (set _G.seen true)) (if _G.seen [{:step 1}] []))",
    );
    if (!compiled.ok) throw new Error(compiled.error.message);
    expect(rt.run(compiled.pattern, 0)).toMatchObject({ ok: true, rawHits: [{ step: 1 }] });
    expect(rt.run(compiled.pattern, 1)).toMatchObject({ ok: true, rawHits: [] });
  });

  it("removes unsafe globals", async () => {
    const result = await runSource(
      "(fn pattern [bar] [{:io (type io) :os (type os) :require (type require)}])",
    );
    expect(result).toMatchObject({
      ok: true,
      rawHits: [{ io: "nil", os: "nil", require: "nil" }],
    });
  });

  it("reports the line of a compile error", async () => {
    const result = await runSource("(bpm 120)\n\n(fn pattern [bar]\n  [{:step 0)");
    expect(result.ok).toBe(false);
    if (!result.ok) {
      expect(result.error.line).toBeGreaterThan(0);
      expect(result.error.message).not.toMatch(/\u001b/);
    }
  });

  it("reports the line and bar of a runtime error", async () => {
    const result = await runSource("(fn pattern [bar]\n  (+ nil 1))", 2);
    expect(result.ok).toBe(false);
    if (!result.ok) expect(result.error).toMatchObject({ line: 2, bar: 2 });
  });

  it("requires the last form to be the pattern function", async () => {
    const result = await runSource("(fn pattern [bar] [])\n42");
    expect(result).toMatchObject({
      ok: false,
      error: { message: "source did not yield a pattern function (last form must be `pattern`)" },
    });
  });

  it("rejects a hit that is not a table", async () => {
    const result = await runSource("(fn pattern [bar] [1])");
    expect(result).toMatchObject({ ok: false, error: { message: "hit 1 is not a table" } });
  });
});
