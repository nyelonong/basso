import { defineConfig, searchForWorkspaceRoot } from "vite";

export default defineConfig({
  server: {
    fs: {
      allow: [
        searchForWorkspaceRoot(process.cwd()),
        "../internal/engine/fennel",
        "../patterns",
        "../sound/808",
      ],
    },
  },
  build: {
    // Samples stay separate files so the browser caches and decodes them individually.
    assetsInlineLimit: (file) => (file.endsWith(".wav") ? false : undefined),
  },
});
