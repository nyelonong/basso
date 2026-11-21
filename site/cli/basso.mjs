#!/usr/bin/env node
// Runs the browser player's engine in Node. Vite loads the TypeScript sources
// so the CLI uses exactly the code the website ships.
import { dirname, resolve } from "node:path";
import { fileURLToPath } from "node:url";
import { createServer } from "vite";

const root = resolve(dirname(fileURLToPath(import.meta.url)), "..");
const server = await createServer({
  root,
  configFile: false,
  logLevel: "error",
  appType: "custom",
  server: { middlewareMode: true, hmr: false, watch: null },
});
let code = 1;
try {
  const { main } = await server.ssrLoadModule("/src/cli/node.ts");
  // npm runs scripts from site/, so `npm run basso` passes the caller's folder in INIT_CWD.
  // Any other launch uses its own folder, even inside an unrelated npm script.
  const cwd = process.env.npm_lifecycle_event === "basso" ? (process.env.INIT_CWD ?? process.cwd()) : process.cwd();
  code = await main(process.argv.slice(2), cwd);
} finally {
  await server.close();
}
process.exit(code);
