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
      bar: { bpm: 120, steps: 16, swing: 50, hits: [{ kind: "sample", sample: "kick2.wav", step: 2, velocity: 1, pan: 0.5 }] },
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

  it("reads swing from the pattern", async () => {
    const result = await evaluate("(swing 66)\n" + hitsOf(""));
    expect(result).toMatchObject({ ok: true, bar: { swing: 66 } });
  });

  it("turns seq step strings into sample hits", async () => {
    const result = await evaluate('(fn pattern [bar] (seq "kick2.wav" "x..o | -x" {:pan 0 :velocity 0.8}))');
    expect(result).toMatchObject({
      ok: true,
      bar: {
        hits: [
          { kind: "sample", sample: "kick2.wav", step: 0, velocity: 0.8, pan: 0 },
          { kind: "sample", sample: "kick2.wav", step: 3, velocity: 0.4, pan: 0 },
          { kind: "sample", sample: "kick2.wav", step: 5, velocity: 0.8, pan: 0 },
        ],
      },
    });
  });

  it("turns melody tokens into held notes", async () => {
    const result = await evaluate('(fn pattern [bar] (melody "pluck" "A1 _ _ . C2 | D#2 _"))');
    expect(result).toMatchObject({
      ok: true,
      bar: {
        hits: [
          { kind: "note", note: "A1", instrument: "pluck", step: 0, length: 3 },
          { kind: "note", note: "C2", instrument: "pluck", step: 4, length: 1 },
          { kind: "note", note: "D#2", instrument: "pluck", step: 5, length: 2 },
        ],
      },
    });
  });

  it("stacks lists and single hits", async () => {
    const result = await evaluate(
      '(fn pattern [bar] (stack (seq "kick2.wav" "x.x") {:step 1 :sample "snare.wav"} []))',
    );
    expect(result.ok && result.bar.hits.map((hit) => hit.step)).toEqual([0, 2, 1]);
  });

  it("reports a bad step character", async () => {
    const result = await evaluate('(fn pattern [bar]\n  (seq "kick2.wav" "x?x"))');
    expect(result).toMatchObject({ ok: false, error: { message: "pattern: seq: unknown step ? (use x, o, . or -)" } });
  });

  it("rejects swing outside 50 to 75", async () => {
    const result = await evaluate("(swing 80)\n" + hitsOf(""));
    expect(result).toMatchObject({ ok: false, error: { message: "swing 80 must be in [50,75]" } });
  });

  it("plays the shuffle-groove example, fill bar included", async () => {
    const source = readFileSync(resolve(repo, "patterns/shuffle-groove.fnl"), "utf8");
    for (const bar of [0, 3]) {
      const result = await evaluate(source, bar);
      expect(result).toMatchObject({ ok: true, bar: { swing: 62 } });
    }
  });

  it("refuses options that would move or rename hits", async () => {
    for (const body of ['(seq "kick2.wav" "x.x." {:step 3})', '(melody "bass" "C2 _" {:length 4})']) {
      const result = await evaluate(`(fn pattern [bar] ${body})`);
      expect(result).toMatchObject({ ok: false, error: { message: expect.stringContaining("options cannot set") } });
    }
  });

  it("skips nil in stack and reports a bare hit without a step", async () => {
    const skipped = await evaluate('(fn pattern [bar] (stack (seq "kick2.wav" "x") (when false (seq "snare.wav" "x"))))');
    expect(skipped.ok && skipped.bar.hits.length).toBe(1);
    const missing = await evaluate('(fn pattern [bar] (stack {:sample "kick2.wav"}))');
    expect(missing).toMatchObject({ ok: false, error: { message: "hit 1 :step is required" } });
  });

  it("accepts swing as a numeric string like the Go engine", async () => {
    expect(await evaluate('(swing "62")\n' + hitsOf(""))).toMatchObject({ ok: true, bar: { swing: 62 } });
  });

  it("keeps the helpers when an earlier pattern rewrites the real globals", async () => {
    await evaluate('((load "seq = nil melody = nil stack = nil"))\n' + hitsOf(""));
    const result = await evaluate('(fn pattern [bar] (stack (seq "kick2.wav" "x") (melody "bass" "C2")))');
    expect(result.ok && result.bar.hits.length).toBe(2);
  });
});

