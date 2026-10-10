import { readFileSync } from "node:fs";
import { resolve } from "node:path";
import { describe, expect, it } from "vitest";

const page = readFileSync(resolve(import.meta.dirname, "../index.html"), "utf8");

describe("Basso player page contract", () => {
  it("is the home page for basso.afrani.id", () => {
    expect(page).toContain('<link rel="canonical" href="https://basso.afrani.id/"');
    expect(page).toContain('<meta property="og:url" content="https://basso.afrani.id/"');
    expect(page).toContain('<meta property="og:image" content="https://basso.afrani.id/og.png"');
  });

  it("offers transport controls and a live status region", () => {
    expect(page).toMatch(/<main[\s>]/);
    expect(page).toMatch(/<button[^>]*data-transport/);
    expect(page).toMatch(/aria-live="polite"/);
  });

  it("tells a first-time visitor what to do", () => {
    expect(page).toContain("Press Play");
    expect(page).toContain("next bar");
  });

  it("links to the about page and the source", () => {
    expect(page).toContain('href="/about"');
    expect(page).toContain("https://github.com/nyelonong/basso");
  });

  it("has no AI or hype wording", () => {
    expect(page).not.toMatch(/\bAI\b|supercharge|unleash|seamless|revolutionary/i);
  });
});
