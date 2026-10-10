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

  it("offers a Share button and a visible, announced result area", () => {
    expect(page).toMatch(/<button[^>]*data-share[\s>]/);
    expect(page).toMatch(/<p[^>]*data-share-notice[^>]*role="status"/);
    expect(page).toMatch(/<input[^>]*data-share-field[^>]*readonly/);
  });

  it("has a shared-pattern banner that starts hidden and offers Save a copy", () => {
    expect(page).toMatch(/<div[^>]*data-shared-banner[^>]*role="status"[^>]*hidden/);
    expect(page).toMatch(/<button[^>]*data-shared-save[^>]*>Save a copy<\/button>/);
  });

  it("lists shared patterns under their own heading in the sidebar", () => {
    expect(page).toContain("Shared with you");
    expect(page).toMatch(/data-list="shared"/);
  });

  it("never builds page markup from text, so shared names cannot inject HTML", () => {
    const code = readFileSync(resolve(import.meta.dirname, "../src/player/main.ts"), "utf8");
    expect(code).not.toMatch(/innerHTML|outerHTML|insertAdjacentHTML|document\.write/);
  });

  it("has no AI or hype wording", () => {
    expect(page).not.toMatch(/\bAI\b|supercharge|unleash|seamless|revolutionary/i);
  });
});
