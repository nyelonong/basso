import { randomBytes } from "node:crypto";
import { deflateRawSync } from "node:zlib";
import { afterEach, describe, expect, it, vi } from "vitest";
import type { GridPattern } from "../../src/player/model";
import {
  buildLink,
  decodePayload,
  describeShareError,
  encodePayload,
  MAX_DECODED_BYTES,
  MAX_LINK_LENGTH,
  readPayload,
  ShareError,
  serializePattern,
  sharingSupported,
  type ShareErrorReason,
  type SharedPattern,
} from "../../src/player/share";

const samples = ["kick2.wav", "snare.wav", "cl_hihat.wav"];

const grid: GridPattern = {
  bpm: 130,
  steps: 4,
  rows: [
    {
      sample: "kick2.wav",
      cells: [
        { on: true, velocity: 1 },
        { on: false, velocity: 1 },
        { on: true, velocity: 0.7 },
        { on: false, velocity: 1 },
      ],
    },
  ],
};
const gridPattern: SharedPattern = { name: "frozen grid", kind: "grid", grid };
// Version-1 links as first published. They must keep opening forever.
const FROZEN_GRID_JSON =
  '{"name":"frozen grid","kind":"grid","grid":{"bpm":130,"steps":4,"rows":[{"sample":"kick2.wav","cells":[{"on":true,"velocity":1},{"on":false,"velocity":1},{"on":true,"velocity":0.7},{"on":false,"velocity":1}]}]}}';
const FROZEN_GRID_PAYLOAD =
  "1.fYzLCsIwFAV_Rc76UloVhPsr0kVMUwl5ksQWDfl30boScXUYhjkVXjgFxpzCQ_ndNekJBKP9BMaH3sMVl-jAw6En5KJiBh8JKawZfK7IwkX7ejJamn23igUEqazddPDgkm6KsCgbpC538NBoE7Ow-bf5Tvru9Cca29jaEw";
const FROZEN_CODE_PAYLOAD =
  "1.q1bKS8xNVbJSSivKr0rNU0jOT0lV0lHKzsxLUbJSgvKK80uLkkGKNJIKchUMDQw0Y_I00vIUChJLSlKL8hSikxKLYhWiYzVj8qBCMXlKtQA";

const payloadOf = (json: string) => `1.${deflateRawSync(Buffer.from(json, "utf8")).toString("base64url")}`;
const gridJson = (change: (g: Record<string, unknown>) => void, name: unknown = "x") => {
  const g = JSON.parse(JSON.stringify(grid)) as Record<string, unknown>;
  change(g);
  return payloadOf(JSON.stringify({ name, kind: "grid", grid: g }));
};

async function reasonOf(promise: Promise<unknown>): Promise<ShareErrorReason> {
  try {
    await promise;
  } catch (error) {
    if (error instanceof ShareError) return error.reason;
    throw error;
  }
  throw new Error("expected a ShareError, but decoding succeeded");
}

afterEach(() => vi.unstubAllGlobals());

describe("frozen version-1 format", () => {
  it("serializes a grid pattern to the frozen JSON", () => {
    expect(serializePattern(gridPattern)).toBe(FROZEN_GRID_JSON);
  });

  it("opens the first published grid link", async () => {
    expect(await decodePayload(FROZEN_GRID_PAYLOAD, samples)).toEqual(gridPattern);
  });

  it("recognises a code link as reserved, and never returns its code", async () => {
    expect(await reasonOf(decodePayload(FROZEN_CODE_PAYLOAD, samples))).toBe("code-unsupported");
  });
});

describe("round trip", () => {
  it("keeps a grid pattern exactly", async () => {
    const payload = await encodePayload(gridPattern);
    expect(payload.startsWith("1.")).toBe(true);
    expect(await decodePayload(payload, samples)).toEqual(gridPattern);
  });

  it("keeps non-ASCII names", async () => {
    const pattern: SharedPattern = { ...gridPattern, name: "gamelan ꦧꦱꦺꦴ ♪ ritme" };
    expect(await decodePayload(await encodePayload(pattern), samples)).toEqual(pattern);
  });

  it("keeps a grid with every row and a long step list", async () => {
    const full: GridPattern = {
      bpm: 90,
      steps: 32,
      rows: samples.map((sample, r) => ({
        sample,
        cells: Array.from({ length: 32 }, (_, i) => ({ on: (i + r) % 3 === 0, velocity: [1, 0.7, 0.4][i % 3] })),
      })),
    };
    const pattern: SharedPattern = { name: "full", kind: "grid", grid: full };
    expect(await decodePayload(await encodePayload(pattern), samples)).toEqual(pattern);
  });
});

describe("decoding bad input", () => {
  it.each<[string, string, ShareErrorReason]>([
    ["an empty payload", "", "empty"],
    ["an unknown version", "2.abc", "version"],
    ["a payload with no version", "abc", "version"],
    ["characters outside base64url", "1.abc+/=", "encoding"],
    ["a base64 length that cannot exist", "1.a", "encoding"],
    ["bytes that are not deflate", `1.${randomBytes(64).toString("base64url")}`, "compressed"],
    ["JSON that is not an object", payloadOf("[1,2]"), "shape"],
    ["text that is not JSON", payloadOf("not json"), "shape"],
    ["an unknown kind", payloadOf('{"name":"x","kind":"video"}'), "shape"],
    ["a code pattern", payloadOf('{"name":"x","kind":"code","source":"(fn pattern [bar] [])"}'), "code-unsupported"],
    ["a payload longer than any link", `1.${"A".repeat(8001)}`, "decoded-too-large"],
    ["invalid UTF-8", `1.${deflateRawSync(Buffer.from([0x7b, 0xff, 0xfe])).toString("base64url")}`, "shape"],
  ])("rejects %s", async (_label, payload, reason) => {
    expect(await reasonOf(decodePayload(payload, samples))).toBe(reason);
  });

  it.each([
    ["a missing name", undefined],
    ["a name of the wrong type", 5],
    ["an empty name", ""],
    ["a name of only spaces", "   "],
    ["a name of 101 characters", "a".repeat(101)],
    ["a name with a newline", "a\nb"],
    ["a name with a right-to-left override", "evil\u202Efdp.exe"],
    ["a name with zero-width characters", "a\u200Bb"],
    ["a name with a line separator", "a\u2028b"],
    ["a name with a lone surrogate", "a\ud800b"],
    ["a name of only a Hangul filler", "\u3164"],
    ["a name of only a braille blank", "\u2800"],
    ["a name of only fillers and spaces", "\u115F\u1160 \uFFA0"],
    ["a name of only a variation selector", "\uFE0F"],
    ["a name of only a combining mark", "\u034F\u0301"],
  ])("rejects %s", async (_label, name) => {
    const json = JSON.stringify({ name, kind: "grid", grid });
    expect(await reasonOf(decodePayload(payloadOf(json), samples))).toBe("shape");
  });

  it.each(["kick", "ꦧꦱꦺꦴ", "e\u0301", "100 BPM ♪", "גרוב"])("accepts the name %s", async (name) => {
    const pattern: SharedPattern = { ...gridPattern, name };
    expect((await decodePayload(await encodePayload(pattern), samples)).name).toBe(name);
  });

  it("accepts a name of exactly 100 characters", async () => {
    const pattern: SharedPattern = { ...gridPattern, name: "a".repeat(100) };
    expect((await decodePayload(await encodePayload(pattern), samples)).name).toHaveLength(100);
  });

  it("rejects a tiny input that expands past 64 KB", async () => {
    const bomb = deflateRawSync(Buffer.alloc(5_000_000, 0x20));
    expect(bomb.length).toBeLessThan(10_000);
    expect(await reasonOf(decodePayload(`1.${bomb.toString("base64url")}`, samples))).toBe("decoded-too-large");
  });

  it.each<[string, string]>([
    ["steps 257", gridJson((g) => (g.steps = 257))],
    ["steps 0", gridJson((g) => (g.steps = 0))],
    ["a fractional step count", gridJson((g) => (g.steps = 4.5))],
    ["BPM 19", gridJson((g) => (g.bpm = 19))],
    ["a fractional BPM", gridJson((g) => (g.bpm = 120.5))],
    ["a sample that does not exist", gridJson((g) => ((g.rows as { sample: string }[])[0].sample = "nope.wav"))],
    ["more rows than samples", gridJson((g) => (g.rows = samples.concat("x.wav").map((s) => ({ sample: s, cells: [] }))))],
    ["a cell list of the wrong length", gridJson((g) => ((g.rows as { cells: unknown[] }[])[0].cells = []))],
    ["velocity 2", gridJson((g) => ((g.rows as { cells: { velocity: number }[] }[])[0].cells[0].velocity = 2))],
    ["a cell that is not on or off", gridJson((g) => ((g.rows as { cells: { on: unknown }[] }[])[0].cells[0].on = "yes"))],
    ["rows that are not a list", gridJson((g) => (g.rows = "kick"))],
    ["a grid that is missing", payloadOf('{"name":"x","kind":"grid"}')],
    ["an off cell with infinite velocity", payloadOf('{"name":"x","kind":"grid","grid":{"bpm":120,"steps":2,"rows":[{"sample":"kick2.wav","cells":[{"on":false,"velocity":1e999},{"on":false,"velocity":1}]}]}}')],
    ["an off cell with negative velocity", gridJson((g) => ((g.rows as { cells: { on: boolean; velocity: number }[] }[])[0].cells[1] = { on: false, velocity: -5 }))],
    ["a sample that does not exist in a row with every cell off", payloadOf('{"name":"x","kind":"grid","grid":{"bpm":120,"steps":1,"rows":[{"sample":"<img src=x onerror=alert(1)>","cells":[{"on":false,"velocity":1}]}]}}')],
    ["two rows using the same sample", gridJson((g) => (g.rows = [(g.rows as unknown[])[0], (g.rows as unknown[])[0]]))],
  ])("rejects a grid with %s", async (_label, payload) => {
    expect(await reasonOf(decodePayload(payload, samples))).toBe("shape");
  });
});

describe("making links", () => {
  it("refuses a pattern that would not decode again", async () => {
    const cells = Array.from({ length: 256 }, () => ({ on: true, velocity: 0.7 }));
    const huge: SharedPattern = {
      name: "x",
      kind: "grid",
      grid: { bpm: 120, steps: 256, rows: Array.from({ length: 20 }, (_, i) => ({ sample: `s${i}.wav`, cells })) },
    };
    expect(new TextEncoder().encode(serializePattern(huge)).length).toBeGreaterThan(MAX_DECODED_BYTES);
    expect(await reasonOf(encodePayload(huge))).toBe("decoded-too-large");
  });

  it("builds a link after the hash", () => {
    expect(buildLink("https://basso.afrani.id/", "1.abc")).toBe("https://basso.afrani.id/#1.abc");
  });

  it("allows exactly 8,000 characters and refuses one more", () => {
    const base = "https://basso.afrani.id/";
    const fits = "1." + "a".repeat(MAX_LINK_LENGTH - base.length - 1 - 2);
    expect(buildLink(base, fits)).toHaveLength(MAX_LINK_LENGTH);
    expect(() => buildLink(base, fits + "a")).toThrowError(ShareError);
    try {
      buildLink(base, fits + "a");
    } catch (error) {
      expect((error as ShareError).reason).toBe("link-too-large");
    }
  });
});

describe("reading the address", () => {
  it.each([
    ["#1.abc", "1.abc"],
    ["1.abc", "1.abc"],
    ["", null],
    ["#", null],
  ])("reads %j", (hash, expected) => {
    expect(readPayload(hash)).toBe(expected);
  });
});

describe("browser support and messages", () => {
  it("reports support when the stream API exists, and not when it is missing", () => {
    expect(sharingSupported()).toBe(true);
    vi.stubGlobal("CompressionStream", undefined);
    expect(sharingSupported()).toBe(false);
  });

  it.each<ShareErrorReason>([
    "empty",
    "version",
    "encoding",
    "compressed",
    "decoded-too-large",
    "shape",
    "code-unsupported",
    "link-too-large",
    "unsupported",
  ])("has a plain message for %s", (reason) => {
    const message = describeShareError(new ShareError(reason, "detail"));
    expect(message.length).toBeGreaterThan(10);
    expect(message).not.toMatch(/undefined|detail/);
  });
});
