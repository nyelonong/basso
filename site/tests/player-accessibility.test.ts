import { readFileSync } from "node:fs";
import { resolve } from "node:path";
import { describe, expect, it } from "vitest";

const read = (path: string) => readFileSync(resolve(import.meta.dirname, "..", path), "utf8");
const playerCss = read("src/player/player.css");
const gridCss = read("src/player/grid-view.css");
const page = read("index.html");

function token(name: string) {
  const match = playerCss.match(new RegExp(`--${name}:\\s*(#[0-9a-f]{6})`, "i"));
  if (!match?.[1]) throw new Error(`missing color token: ${name}`);
  return match[1];
}

function luminance(hex: string) {
  const [r, g, b] = [1, 3, 5].map((offset) => {
    const c = Number.parseInt(hex.slice(offset, offset + 2), 16) / 255;
    return c <= 0.04045 ? c / 12.92 : ((c + 0.055) / 1.055) ** 2.4;
  });
  return r * 0.2126 + g * 0.7152 + b * 0.0722;
}

function contrast(a: string, b: string) {
  const [hi, lo] = [luminance(token(a)), luminance(token(b))].sort((x, y) => y - x);
  return (hi + 0.05) / (lo + 0.05);
}

function rule(css: string, selector: string) {
  const start = css.indexOf(`${selector} {`);
  if (start < 0) throw new Error(`missing rule: ${selector}`);
  return css.slice(start, css.indexOf("}", start));
}

describe("player accessibility contract", () => {
  it.each([
    ["panel-text", "panel"],
    ["panel-text", "panel-raised"],
    ["panel-muted", "panel"],
    ["panel-muted", "panel-raised"],
    ["error", "panel"],
    ["ink", "accent"],
    ["ink", "paper"],
  ])("keeps %s readable on %s (WCAG AA)", (fg, bg) => {
    expect(contrast(fg, bg)).toBeGreaterThanOrEqual(4.5);
  });

  it("gives every grid cell and control a 44px touch target", () => {
    expect(rule(gridCss, ".grid-cell")).toContain("min-height: 44px");
    expect(rule(gridCss, ".grid-remove")).toContain("min-width: 44px");
    expect(rule(playerCss, ".library-item")).toContain("min-height: 44px");
  });

  it("switches quiet cells to light text so their step numbers stay readable", () => {
    expect(gridCss).toMatch(/\[data-soft="true"\][^{]*\{[^}]*color: var\(--panel-text\)/);
  });

  it("keeps the code editor behind a toggle on phones", () => {
    expect(page).toMatch(/<button[^>]*data-code-toggle[^>]*aria-expanded="false"/);
    expect(playerCss).toMatch(/@media \(max-width: 560px\)[\s\S]*\.code-mode:not\(\[data-code-open="true"\]\) \.editor/);
  });
});
