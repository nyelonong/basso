import { readFileSync } from "node:fs";
import { resolve } from "node:path";
import { describe, expect, it } from "vitest";
import { fennelProvider } from "../../src/player/evaluate";
import { createFennelRuntime } from "../../src/player/fennel";

const repo = resolve(import.meta.dirname, "../../..");
const compilerSource = readFileSync(resolve(repo, "internal/engine/fennel/compiler.lua"), "utf8");
const samples = ["kick2.wav", "snare.wav", "cl_hihat.wav", "open_hh.wav", "handclap.wav", "conga1.wav", "cowbell.wav"];
const runtime = createFennelRuntime(compilerSource);

async function evaluate(source: string, bar = 0, random = () => 0.75) {
  const rt = await runtime;
  const compiled = rt.compile(source);
  if (!compiled.ok) throw new Error(compiled.error.message);
  return fennelProvider(rt, compiled.pattern, samples, random)(bar);
}

const hitsOf = (body: string) => `(fn pattern [bar] [${body}])`;

describe("fennelProvider", () => {
  it("applies the Go engine defaults to sample hits", async () => {
    const result = await evaluate(hitsOf('{:step 2 :sample "kick2.wav"}'));
    expect(result).toEqual({
      ok: true,
      bar: { bpm: 120, steps: 16, hits: [{ kind: "sample", sample: "kick2.wav", step: 2, velocity: 1, pan: 0.5 }] },
    });
  });

  it("applies the Go engine defaults to note hits", async () => {
    const result = await evaluate(hitsOf('{:step 0 :note "C2"}'));
    expect(result).toMatchObject({
      ok: true,
      bar: { hits: [{ kind: "note", note: "C2", instrument: "bass", length: 1, step: 0, velocity: 1, pan: 0 }] },
    });
  });

  it("keeps explicit fields", async () => {
    const result = await evaluate(
      '(bpm 140) (steps 8)\n' +
        hitsOf('{:step 1 :note "E4" :instrument "lead" :length 2 :velocity 0.5 :pan -0.25}'),
    );
    expect(result).toMatchObject({
      ok: true,
      bar: {
        bpm: 140,
        steps: 8,
        hits: [{ kind: "note", note: "E4", instrument: "lead", length: 2, step: 1, velocity: 0.5, pan: -0.25 }],
      },
    });
  });

  it("truncates fractional numbers the way the Go engine does", async () => {
    const result = await evaluate(hitsOf('{:step 3.9 :note "C2" :length 2.7}'));
    expect(result).toMatchObject({ ok: true, bar: { hits: [{ step: 3, length: 2 }] } });
  });

  it.each([
    [hitsOf('{:step 0 :sample "kick2.wav" :note "C2"}'), "hit 1 must have exactly one of :sample or :note"],
    [hitsOf("{:step 0}"), "hit 1 must have exactly one of :sample or :note"],
    [hitsOf('{:sample "kick2.wav"}'), "hit 1 :step is required"],
    [hitsOf('{:step "one" :sample "kick2.wav"}'), "hit 1 :step must be a number"],
    [hitsOf('{:step 0 :sample "kick2.wav" :velocity "loud"}'), "hit 1 :velocity must be a number"],
  ])("rejects %s", async (source, message) => {
    expect(await evaluate(source, 4)).toEqual({ ok: false, error: { message, bar: 4 } });
  });

  it("reports validation failures with the bar number", async () => {
    expect(await evaluate(hitsOf('{:step 0 :sample "nope.wav"}'), 2)).toEqual({
      ok: false,
      error: { message: 'hit 0 sample "nope.wav" is not in the sound inventory', bar: 2 },
    });
  });

  it("passes runtime errors through", async () => {
    const result = await evaluate("(fn pattern [bar]\n  (error \"boom\"))", 1);
    expect(result).toMatchObject({ ok: false, error: { line: 2, bar: 1 } });
  });

  it("evaluates a full example pattern", async () => {
    const result = await evaluate(readFileSync(resolve(repo, "patterns/basic-groove.fnl"), "utf8"));
    expect(result.ok).toBe(true);
    if (result.ok) expect(result.bar.hits.length).toBeGreaterThan(10);
  });
});
