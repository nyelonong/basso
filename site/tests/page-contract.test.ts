import { readFileSync } from "node:fs";
import { resolve } from "node:path";
import { describe, expect, it } from "vitest";

const page = readFileSync(resolve(import.meta.dirname, "../about.html"), "utf8");

describe("Basso about page contract", () => {
  it("describes Basso's real playback promise", () => {
    expect(page).toContain("Save the pattern. Keep the beat.");
    expect(page).toContain("next bar");
    expect(page).toContain("without stopping the music");
  });

  it("leads with the browser player", () => {
    expect(page).toContain('<a class="button button-primary" href="/">Open the player</a>');
  });

  it("does not describe AI features the browser player does not have", () => {
    expect(page).not.toMatch(/studio|provider|candidate/i);
  });

  it("says a grid beat can be sent as a link, with no account or server", () => {
    expect(page).toContain("as a link");
    expect(page).toContain("no account or server");
    expect(page).not.toMatch(/share (a )?code/i);
  });

  it("keeps the command-line version as a frozen secondary option", () => {
    expect(page).toContain('id="cli"');
    expect(page).toContain("It is frozen");
    expect(page).toContain(
      "go install github.com/nyelonong/basso/cmd/basso@latest",
    );
    expect(page).toContain("https://github.com/nyelonong/basso");
  });

  it("publishes canonical and social metadata for basso.afrani.id", () => {
    expect(page).toContain('<link rel="canonical" href="https://basso.afrani.id/about"');
    expect(page).toContain('<meta property="og:url" content="https://basso.afrani.id/about"');
    expect(page).toContain('<meta property="og:image" content="https://basso.afrani.id/og.png"');
  });

  it("keeps the primary page landmarks accessible and links to the player", () => {
    expect(page).toMatch(/<header[\s>]/);
    expect(page).toMatch(/<main[\s>]/);
    expect(page).toMatch(/<footer[\s>]/);
    expect(page).toContain('href="/"');
    expect(page).toContain('aria-live="polite"');
  });

  it("does not fall back to generic AI landing-page copy", () => {
    expect(page).not.toMatch(/supercharge|unleash|seamless|revolutionary/i);
  });
});
