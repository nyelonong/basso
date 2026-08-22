import { readFileSync } from "node:fs";
import { resolve } from "node:path";
import { describe, expect, it } from "vitest";

const page = readFileSync(resolve(import.meta.dirname, "../index.html"), "utf8");

describe("Basso landing page contract", () => {
  it("describes Basso's real playback promise", () => {
    expect(page).toContain("Save the pattern. Keep the beat.");
    expect(page).toContain("next bar");
    expect(page).toContain("without restarting audio");
  });

  it("offers a real install path and source repository", () => {
    expect(page).toContain(
      "go install github.com/nyelonong/basso/cmd/basso@latest",
    );
    expect(page).toContain("https://github.com/nyelonong/basso");
  });

  it("publishes canonical and social metadata for basso.afrani.id", () => {
    expect(page).toContain('<link rel="canonical" href="https://basso.afrani.id/"');
    expect(page).toContain('<meta property="og:url" content="https://basso.afrani.id/"');
    expect(page).toContain('<meta property="og:image" content="https://basso.afrani.id/og.png"');
  });

  it("keeps the primary page landmarks and sequencer controls accessible", () => {
    expect(page).toMatch(/<header[\s>]/);
    expect(page).toMatch(/<main[\s>]/);
    expect(page).toMatch(/<footer[\s>]/);
    expect(page).toContain('aria-label="One-bar 808 sequencer"');
    expect(page).toContain('aria-live="polite"');
  });

  it("does not fall back to generic AI landing-page copy", () => {
    expect(page).not.toMatch(/supercharge|unleash|seamless|revolutionary/i);
  });
});
