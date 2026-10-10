import { gridToBar } from "./grid";
import type { GridPattern } from "./model";
import { validateBar } from "./validate";

// Only grid patterns can be shared. A grid is checked data, so opening a link never
// runs anyone's code. The format reserves kind "code" for later; this decoder refuses it.
export type SharedPattern = { name: string; kind: "grid"; grid: GridPattern };

export type ShareErrorReason =
  | "empty"
  | "version"
  | "encoding"
  | "compressed"
  | "decoded-too-large"
  | "shape"
  | "code-unsupported"
  | "link-too-large"
  | "unsupported";

export class ShareError extends Error {
  constructor(
    readonly reason: ShareErrorReason,
    detail: string,
  ) {
    super(detail);
    this.name = "ShareError";
  }
}

export const MAX_LINK_LENGTH = 8000;
export const MAX_DECODED_BYTES = 65536;
export const MAX_NAME_LENGTH = 100;

const VERSION = "1";

const MESSAGES: Record<ShareErrorReason, string> = {
  empty: "This link has no pattern in it.",
  version: "This link was made by a newer version of Basso.",
  encoding: "This link is damaged, so the pattern could not be read.",
  compressed: "This link is damaged, so the pattern could not be read.",
  "decoded-too-large": "This pattern is too large to open.",
  shape: "This link does not hold a valid pattern.",
  "code-unsupported": "This link holds a code pattern, which cannot be opened yet. Ask for an exported .fnl file instead.",
  "link-too-large": "This pattern is too long to share as a link. Use Export .fnl instead.",
  unsupported: "Sharing needs a newer browser.",
};

export function describeShareError(error: ShareError): string {
  return MESSAGES[error.reason];
}

export function sharingSupported(): boolean {
  return typeof CompressionStream === "function" && typeof DecompressionStream === "function";
}

// Key order is fixed so a version-1 link always holds the same JSON for the same pattern.
export function serializePattern(pattern: SharedPattern): string {
  const { bpm, steps, rows } = pattern.grid;
  return JSON.stringify({
    name: pattern.name,
    kind: "grid",
    grid: {
      bpm,
      steps,
      rows: rows.map((row) => ({
        sample: row.sample,
        cells: row.cells.map((cell) => ({ on: cell.on, velocity: cell.velocity })),
      })),
    },
  });
}

function toBase64Url(bytes: Uint8Array<ArrayBuffer>): string {
  let binary = "";
  for (let i = 0; i < bytes.length; i += 0x8000) {
    binary += String.fromCharCode(...bytes.subarray(i, i + 0x8000));
  }
  return btoa(binary).replace(/\+/g, "-").replace(/\//g, "_").replace(/=+$/, "");
}

function fromBase64Url(text: string): Uint8Array<ArrayBuffer> {
  if (!/^[A-Za-z0-9_-]*$/.test(text) || text.length % 4 === 1) {
    throw new ShareError("encoding", "payload is not base64url");
  }
  const binary = atob(text.replace(/-/g, "+").replace(/_/g, "/"));
  return Uint8Array.from(binary, (char) => char.charCodeAt(0));
}

async function compress(bytes: Uint8Array<ArrayBuffer>): Promise<Uint8Array<ArrayBuffer>> {
  const stream = new Blob([bytes]).stream().pipeThrough(new CompressionStream("deflate-raw"));
  return new Uint8Array(await new Response(stream).arrayBuffer());
}

// Reads chunk by chunk and stops as soon as the limit is passed, so a tiny input
// that expands enormously is never fully inflated.
async function decompress(bytes: Uint8Array<ArrayBuffer>): Promise<Uint8Array<ArrayBuffer>> {
  const reader = new Blob([bytes]).stream().pipeThrough(new DecompressionStream("deflate-raw")).getReader();
  const chunks: Uint8Array[] = [];
  let total = 0;
  try {
    for (;;) {
      const { done, value } = await reader.read();
      if (done) break;
      total += value.length;
      if (total > MAX_DECODED_BYTES) {
        await reader.cancel().catch(() => undefined);
        throw new ShareError("decoded-too-large", `more than ${MAX_DECODED_BYTES} bytes`);
      }
      chunks.push(value);
    }
  } catch (error) {
    if (error instanceof ShareError) throw error;
    throw new ShareError("compressed", "payload is not valid deflate data");
  }
  const out = new Uint8Array(total);
  let offset = 0;
  for (const chunk of chunks) {
    out.set(chunk, offset);
    offset += chunk.length;
  }
  return out;
}

export async function encodePayload(pattern: SharedPattern): Promise<string> {
  const bytes = new TextEncoder().encode(serializePattern(pattern));
  if (bytes.length > MAX_DECODED_BYTES) {
    throw new ShareError("decoded-too-large", `pattern is ${bytes.length} bytes`);
  }
  return `${VERSION}.${toBase64Url(await compress(bytes))}`;
}

export function buildLink(base: string, payload: string): string {
  const link = `${base}#${payload}`;
  if (link.length > MAX_LINK_LENGTH) {
    throw new ShareError("link-too-large", `link is ${link.length} characters`);
  }
  return link;
}

// Only a fragment that starts with a version tag is a link; others, such as the
// skip link's "#main", belong to the page.
export function readPayload(hash: string): string | null {
  const payload = hash.startsWith("#") ? hash.slice(1) : hash;
  return /^\d+\./.test(payload) ? payload : null;
}

const shapeError = (detail: string) => new ShareError("shape", detail);

// Control, format (bidi overrides, zero width), line/paragraph separator and lone
// surrogate characters can hide or spoof a name, so none are allowed.
const FORBIDDEN_NAME_CHARACTERS = /[\p{Cc}\p{Cf}\p{Zl}\p{Zp}\p{Cs}]/u;

// A name needs at least one character that shows: not whitespace, not an invisible
// filler or variation selector, not the blank braille cell, and not a lone combining mark.
const VISIBLE_NAME_CHARACTER = /[^\s\p{Default_Ignorable_Code_Point}\p{M}\u2800]/u;

const FALLBACK_NAME = "shared pattern";

// Makes a user-typed name acceptable to decodePayload, so a link always opens for
// whoever receives it.
export function shareableName(name: string): string {
  const cleaned = Array.from(name.replace(new RegExp(FORBIDDEN_NAME_CHARACTERS.source, "gu"), "")).reduce(
    (kept, char) => (kept.length + char.length <= MAX_NAME_LENGTH ? kept + char : kept),
    "",
  ).trim();
  return VISIBLE_NAME_CHARACTER.test(cleaned) ? cleaned : FALLBACK_NAME;
}

const isObject = (value: unknown): value is Record<string, unknown> =>
  typeof value === "object" && value !== null && !Array.isArray(value);

function parseGrid(value: unknown, sampleNames: readonly string[]): GridPattern {
  if (!isObject(value)) throw shapeError("grid is not an object");
  const { bpm, steps, rows } = value;
  if (!Number.isInteger(bpm) || !Number.isInteger(steps)) throw shapeError("bpm and steps must be whole numbers");
  if (!Array.isArray(rows) || rows.length > sampleNames.length) throw shapeError("rows are not a list of samples");

  const seen = new Set<string>();
  const grid: GridPattern = {
    bpm: bpm as number,
    steps: steps as number,
    rows: rows.map((row) => {
      if (!isObject(row) || typeof row.sample !== "string" || !Array.isArray(row.cells)) {
        throw shapeError("row is malformed");
      }
      if (!sampleNames.includes(row.sample)) throw shapeError("row names a sample that does not exist");
      if (seen.has(row.sample)) throw shapeError("two rows use the same sample");
      seen.add(row.sample);
      if (row.cells.length !== steps) throw shapeError("row has the wrong number of cells");
      return {
        sample: row.sample,
        cells: row.cells.map((cell: unknown) => {
          if (!isObject(cell) || typeof cell.on !== "boolean" || typeof cell.velocity !== "number") {
            throw shapeError("cell is malformed");
          }
          if (!(cell.velocity >= 0 && cell.velocity <= 1)) throw shapeError("cell velocity is out of range");
          return { on: cell.on, velocity: cell.velocity };
        }),
      };
    }),
  };
  const invalid = validateBar(gridToBar(grid), sampleNames);
  if (invalid !== null) throw shapeError(invalid);
  return grid;
}

export async function decodePayload(payload: string, sampleNames: readonly string[]): Promise<SharedPattern> {
  if (payload === "") throw new ShareError("empty", "no payload");
  if (payload.length > MAX_LINK_LENGTH) throw new ShareError("decoded-too-large", "payload is longer than any link made here");
  const dot = payload.indexOf(".");
  if (dot < 0 || payload.slice(0, dot) !== VERSION) throw new ShareError("version", "unknown version");

  const bytes = await decompress(fromBase64Url(payload.slice(dot + 1)));
  let parsed: unknown;
  try {
    parsed = JSON.parse(new TextDecoder("utf-8", { fatal: true }).decode(bytes));
  } catch {
    throw shapeError("payload is not JSON text");
  }

  if (!isObject(parsed)) throw shapeError("payload is not an object");
  const { name, kind } = parsed;
  if (
    typeof name !== "string" ||
    name.length > MAX_NAME_LENGTH ||
    !VISIBLE_NAME_CHARACTER.test(name) ||
    FORBIDDEN_NAME_CHARACTERS.test(name)
  ) {
    throw shapeError("name is missing or not allowed");
  }
  if (kind === "code") throw new ShareError("code-unsupported", "code patterns cannot be opened from a link");
  if (kind === "grid") return { name, kind, grid: parseGrid(parsed.grid, sampleNames) };
  throw shapeError("unknown kind");
}
