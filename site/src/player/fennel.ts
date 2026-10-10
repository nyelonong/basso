import { LuaFactory, type LuaEngine } from "wasmoon";
import type { Diagnostic } from "./model";

export type CompiledPattern = { readonly lua: string };

export type CompileResult =
  | { ok: true; pattern: CompiledPattern }
  | { ok: false; error: Diagnostic };

export type RunResult =
  | { ok: true; bpm: number; steps: number; rawHits: Record<string, unknown>[] }
  | { ok: false; error: Diagnostic };

export interface FennelRuntime {
  compile(source: string): CompileResult;
  run(pattern: CompiledPattern, bar: number): RunResult;
  close(): void;
}

const CHUNK_NAME = "pattern.fnl";

// Mirrors removeUnsafeGlobals in internal/engine/fennel.go.
const UNSAFE_GLOBALS = [
  "os",
  "io",
  "debug",
  "package",
  "require",
  "dofile",
  "loadfile",
  "channel",
  "coroutine",
];

const BRIDGE = `
local fennel = assert(load(__basso_compiler_source, "=fennel"))()
__basso_compiler_source = nil
bpm = function() end
steps = function() end
for _, name in ipairs(__basso_unsafe) do _G[name] = nil end
__basso_unsafe = nil

local compile_string = fennel.compileString or fennel["compile-string"]
local library_names = { string = true, table = true, math = true, utf8 = true }

local function fresh_env()
  local env = {}
  for key, value in pairs(_G) do
    if type(key) == "string" and key:sub(1, 8) ~= "__basso_" then
      if library_names[key] then
        local copy = {}
        for k, v in pairs(value) do copy[k] = v end
        env[key] = copy
      else
        env[key] = value
      end
    end
  end
  env._G = env
  return env
end

function __basso_compile(source)
  local ok, result = pcall(compile_string, source, { filename = "${CHUNK_NAME}", correlate = true })
  if not ok then return { ok = false, message = "compile source: " .. tostring(result) } end
  local chunk, err = load(result, "=${CHUNK_NAME}", "t", {})
  if not chunk then return { ok = false, message = "load compiled source: " .. tostring(err) } end
  return { ok = true, lua = result }
end

function __basso_run(lua_source, bar)
  local bpm_value, steps_value = 120, 16
  local env = fresh_env()
  env.bpm = function(n) bpm_value = math.tointeger(n // 1) or n end
  env.steps = function(n) steps_value = math.tointeger(n // 1) or n end
  local chunk, err = load(lua_source, "=${CHUNK_NAME}", "t", env)
  if not chunk then return { ok = false, message = "load compiled source: " .. tostring(err) } end
  local ok, pattern = pcall(chunk)
  if not ok then return { ok = false, message = "evaluate source: " .. tostring(pattern) } end
  if type(pattern) ~= "function" then
    return { ok = false, message = "source did not yield a pattern function (last form must be \`pattern\`)" }
  end
  local ok2, hits = pcall(pattern, bar)
  if not ok2 then return { ok = false, message = "pattern: " .. tostring(hits) } end
  if type(hits) ~= "table" then return { ok = false, message = "pattern did not return a table" } end
  local rows = {}
  for i, hit in ipairs(hits) do
    if type(hit) ~= "table" then
      return { ok = false, message = "hit " .. i .. " is not a table" }
    end
    local row = {}
    for k, v in pairs(hit) do
      if type(k) == "string" then row[k] = v end
    end
    rows[i] = row
  end
  return { ok = true, bpm = bpm_value, steps = steps_value, count = #rows, hits = rows }
end
`;

function lineOf(message: string): number | undefined {
  const match = message.match(/pattern\.fnl:(\d+)/);
  return match ? Number(match[1]) : undefined;
}

function messageOf(error: unknown): string {
  return error instanceof Error ? error.message : String(error);
}

function diagnostic(message: string, bar?: number): Diagnostic {
  const line = lineOf(message);
  return {
    message,
    ...(line === undefined ? {} : { line }),
    ...(bar === undefined ? {} : { bar }),
  };
}

type RawRun =
  | { ok: false; message: string }
  | { ok: true; bpm: number; steps: number; count: number; hits: Record<string, unknown>[] | Record<number, Record<string, unknown>> };

function toArray(hits: RawRun & { ok: true }): Record<string, unknown>[] {
  if (Array.isArray(hits.hits)) return hits.hits;
  const rows: Record<string, unknown>[] = [];
  for (let i = 1; i <= hits.count; i++) rows.push((hits.hits as Record<number, Record<string, unknown>>)[i]);
  return rows;
}

export async function createFennelRuntime(
  compilerSource: string,
  wasmUrl?: string,
): Promise<FennelRuntime> {
  const engine: LuaEngine = await new LuaFactory(wasmUrl).createEngine({ injectObjects: false });
  engine.global.set("__basso_compiler_source", compilerSource);
  engine.global.set("__basso_unsafe", UNSAFE_GLOBALS);
  engine.doStringSync(BRIDGE);
  const compileFn = engine.global.get("__basso_compile") as (
    source: string,
  ) => { ok: true; lua: string } | { ok: false; message: string };
  const runFn = engine.global.get("__basso_run") as (lua: string, bar: number) => RawRun;

  return {
    compile(source) {
      let compiled: ReturnType<typeof compileFn>;
      try {
        compiled = compileFn(source);
      } catch (error) {
        return { ok: false, error: diagnostic(messageOf(error)) };
      }
      if (!compiled.ok) return { ok: false, error: diagnostic(compiled.message) };
      return { ok: true, pattern: { lua: compiled.lua } };
    },
    run(pattern, bar) {
      let result: RawRun;
      try {
        result = runFn(pattern.lua, bar);
      } catch (error) {
        return { ok: false, error: diagnostic(messageOf(error), bar) };
      }
      if (!result.ok) return { ok: false, error: diagnostic(result.message, bar) };
      return { ok: true, bpm: result.bpm, steps: result.steps, rawHits: toArray(result) };
    },
    close() {
      engine.global.close();
    },
  };
}
