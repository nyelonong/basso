import { resolve } from "node:path";
import { build, type Rollup } from "vite";
import { describe, expect, it } from "vitest";

describe("player bundle", () => {
  it("keeps the Fennel compiler and CodeMirror out of the first download", async () => {
    const output = (await build({
      root: resolve(import.meta.dirname, "../.."),
      logLevel: "silent",
      build: { write: false },
    })) as Rollup.RollupOutput | Rollup.RollupOutput[];
    const chunks = (Array.isArray(output) ? output : [output])
      .flatMap((o) => o.output)
      .filter((f): f is Rollup.OutputChunk => f.type === "chunk");
    const entry = chunks.find((c) => c.isEntry && c.facadeModuleId?.endsWith("index.html"));
    if (!entry) throw new Error("no player entry chunk");
    const firstLoad = [entry, ...entry.imports.map((name) => chunks.find((c) => c.fileName === name)!)];
    const firstCode = firstLoad.map((c) => c.code).join("\n");

    expect(firstCode).not.toContain("compileString");
    expect(firstCode).not.toContain("cm-content");
    const all = chunks.map((c) => c.code).join("\n");
    expect(all).toContain("compileString");
    expect(all).toContain("cm-content");
  }, 60_000);
});
