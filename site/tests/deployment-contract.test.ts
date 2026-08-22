import { readFileSync } from "node:fs";
import { resolve } from "node:path";
import { describe, expect, it } from "vitest";

const root = resolve(import.meta.dirname, "..");
const packageJson = JSON.parse(readFileSync(resolve(root, "package.json"), "utf8"));

describe("Cloudflare deployment contract", () => {
  it("builds static assets before an assets-only Worker deploy", () => {
    const wrangler = JSON.parse(
      readFileSync(resolve(root, "wrangler.jsonc"), "utf8"),
    );

    expect(packageJson.scripts.build).toBe("vite build");
    expect(packageJson.scripts.check).toContain("wrangler deploy --dry-run");
    expect(wrangler.name).toBe("basso");
    expect(wrangler.assets.directory).toBe("./dist");
  });

  it("documents the exact Workers Builds and custom-domain handoff", () => {
    const readme = readFileSync(resolve(root, "README.md"), "utf8");

    expect(readme).toContain("Root directory: `site`");
    expect(readme).toContain("Build command: `npm run build`");
    expect(readme).toContain("Deploy command: `npx wrangler deploy`");
    expect(readme).toContain("Worker name: `basso`");
    expect(readme).toContain("Custom domain: `basso.afrani.id`");
  });
});
