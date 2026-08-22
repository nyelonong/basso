import { readFileSync } from "node:fs";
import { resolve } from "node:path";
import { describe, expect, it } from "vitest";
import { stepLabel } from "../src/sequencer";

const styles = readFileSync(
  resolve(import.meta.dirname, "../src/styles.css"),
  "utf8",
);

function token(name: string) {
  const match = styles.match(new RegExp(`--${name}:\\s*(#[0-9a-f]{6})`, "i"));
  if (!match?.[1]) {
    throw new Error(`missing color token: ${name}`);
  }
  return match[1];
}

function luminance(hex: string) {
  const channels = [1, 3, 5].map((offset) => {
    const encoded = Number.parseInt(hex.slice(offset, offset + 2), 16) / 255;
    return encoded <= 0.04045
      ? encoded / 12.92
      : ((encoded + 0.055) / 1.055) ** 2.4;
  });
  return channels[0]! * 0.2126 + channels[1]! * 0.7152 + channels[2]! * 0.0722;
}

function contrast(left: string, right: string) {
  const values = [luminance(left), luminance(right)].sort((a, b) => b - a);
  return (values[0]! + 0.05) / (values[1]! + 0.05);
}

describe("accessibility contract", () => {
  it("keeps accent labels readable on the paper surface", () => {
    expect(contrast(token("accent-ink"), token("paper"))).toBeGreaterThanOrEqual(4.5);
  });

  it("includes the visible step number in each accessible button name", () => {
    const label = stepLabel("kick", 0);

    expect(label.visible).toBe("01");
    expect(label.accessible).toContain(label.visible);
    expect(label.accessible).toContain("kick step 1");
  });
});
