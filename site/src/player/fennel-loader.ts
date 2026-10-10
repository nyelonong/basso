import compilerSource from "../../../internal/engine/fennel/compiler.lua?raw";
import wasmUrl from "wasmoon/dist/glue.wasm?url";
import { createFennelRuntime, type FennelRuntime } from "./fennel";

export const FENNEL_COMPILER_SOURCE: string = compilerSource;

export function loadFennelRuntime(): Promise<FennelRuntime> {
  return createFennelRuntime(compilerSource, wasmUrl);
}
