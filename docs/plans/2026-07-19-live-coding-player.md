# Plan: basso live-coding player

## Goal

Turn basso into a live-coding player: a persistent process that plays a Fennel
pattern continuously and reloads it at the next bar boundary when the source
file is saved, with no audio restart. Implements the spec at
`docs/specs/2026-07-19-live-coding-player.md`.

## Architecture

- **Module** `github.com/nyelonong/basso`, built with the nix devshell (SDL2 +
  PortAudio). Pure-Go deps only: `gopkg.in/mix.v0` (audio, imported
  aliased as `atomix`), `github.com/yuin/gopher-lua` (Lua VM),
  `github.com/fsnotify/fsnotify` (file watch). The Fennel compiler is vendored
  as a Lua file loaded into the gopher-lua VM.
- **Engine** (`internal/engine`): opens the audio device once, runs a bar loop,
  asks a `PatternProvider` for each bar's `Hit`s, and schedules them through an
  `AudioSink` seam. The engine is reload-agnostic.
- **Providers**: `StaticProvider` (the current `m001` pattern, regression
  fixture) and `FennelProvider` (compiles/evals Fennel via gopher-lua, maps hit
  tables to `Hit`, binds `bpm`/`steps` host functions, and owns bar-granular hot
  reload via fsnotify).
- **CLI** (`cmd/basso`): `basso play <file.fnl>`. Ships first wired to
  `StaticProvider` (early end-to-end audio proof), then swaps to
  `FennelProvider.NewFromFile`.
- **Old code** (`const.go`, `m001.go` at repo root) is left in place as a dead
  `package main` through the build, then deleted in a final contract task.

## Global Constraints

Copied verbatim from the spec's Constraints section.

- **Pure-Go dependencies only** in app code — no cgo. The toolchain is nix-based
  and cross-platform. The only native deps are the system audio libraries SDL2
  and PortAudio, provided by a nix devshell (`shell.nix` or flake), **not brew**.
- **Module path is `github.com/nyelonong/basso`.** The audio dependency is
  `gopkg.in/mix.v0`, imported aliased as `atomix` in the engine's audio
  sink.
- **Hot reload is bar-granular.** Pattern changes take effect at the next bar
  boundary, never mid-bar. Do not re-evaluate the script mid-bar.
- **The audio device is opened once at startup and held open for the process
  lifetime.** Never reopen or re-teardown on a pattern change.
- **Pattern scripts are Fennel on gopher-lua.** If Fennel-on-gopher-lua proves
  broken in practice, the fallback is plain Lua on the same gopher-lua VM. Do not
  introduce a second interpreter or any cgo to work around it.
- **Every wave ships with galdr gates green** (`gofmt -l .`, `go vet ./...`,
  `go test ./...`) and a green smoke run. A skipped smoke (no audio device / no
  system libs) is not a pass.
- **TDD.** No production code without a failing test first (galdr Iron Law).

## Progress

| Task | Wave | Status | Evidence |
|------|------|--------|----------|
| 1.1  | 1    | done | `8a5a2eb` — go.mod correct module path + 3 deps; `go build`/`go mod verify` pass |
| 1.2  | 1    | done | `a41872b` — shell.nix has SDL2/PortAudio/Go/pkg-config/sox; `nix-shell --run 'go version'` passes |
| 2.1  | 2    | done | `21a0b2c`,`f4e7703`,`e0cabaa` — Hit/PatternProvider/AudioSink/atomixSink/Engine.Run; 3 tests pass; gates green |
| 2.2  | 2    | done | `466d7a6`,`e86c396`,`0a1c320` — clock seam, ctx-aware bar wait; 5 tests pass in 0.09s; gates green; re-verified empirically (real clock: 1 fire scheduled in 602ms against a non-blocking provider, was 722k/50ms before the fix) |
| 3.1  | 3    | pending | — |
| 4.1  | 4    | pending | — |
| 4.2  | 4    | pending | — |
| 5.1  | 5    | pending | — |
| 6.1  | 6    | pending | — |

## Waves

### Wave 1 — Bootstrap

Spec coverage: spec Wave 0 (project bootstrap).

#### Task 1.1: Go module + dependencies
**Files:** Create `go.mod`, `go.sum`
**Write-scope:** `go.mod`, `go.sum`
**Consumes:** nothing
**Produces:** module `github.com/nyelonong/basso` with deps `gopkg.in/mix.v0`, `github.com/yuin/gopher-lua`, `github.com/fsnotify/fsnotify` — used by 2.1, 4.2, 5.1
**Seams:** none (declarative setup)
**Tests:** none (wave gate proves build)
- [ ] `go mod init github.com/nyelonong/basso`
- [ ] `go get gopkg.in/mix.v0` (import aliased as `atomix`)
- [ ] `go get github.com/yuin/gopher-lua`
- [ ] `go get github.com/fsnotify/fsnotify`
- [ ] Legacy `const.go`/`m001.go` removed (early contract); `go build ./...` succeeds on the legacy-free module
- [ ] Wave gate: `gofmt -l .` clean, `go vet ./...` clean, `go test ./...` passes

#### Task 1.2: nix devshell
**Files:** Create `shell.nix`
**Write-scope:** `shell.nix`
**Consumes:** nothing
**Produces:** a devshell providing SDL2 + PortAudio — used for every build/run/smoke thereafter
**Seams:** none
**Tests:** none (wave gate proves the shell builds Go)
- [ ] Write `shell.nix` exposing `SDL2` and `portaudio` (and `go`) in `buildInputs`
- [ ] Confirm `nix-shell --run 'go build ./...'` succeeds (no brew)
- [ ] Wave gate: gates green inside the nix shell

**Wave 1 gate:** `gofmt -l .` clean; `go vet ./...` clean; `go test ./...` passes; `nix-shell --run 'go build ./...'` succeeds.

---

### Wave 2 — Engine core

Spec coverage: spec Wave 1 (persistent engine + bar loop, PatternProvider, Hit).

#### Task 2.1: Engine, Hit, PatternProvider, AudioSink
**Files:** Create `internal/engine/engine.go`, `internal/engine/sink.go`, `internal/engine/engine_test.go`
**Write-scope:** `internal/engine/engine.go`, `internal/engine/sink.go`, `internal/engine/engine_test.go`
**Consumes:** `gopkg.in/mix.v0` API (Configure, SetSoundsPath, StartAt, SetFire, FireCount, Start, Teardown) + `gopkg.in/mix.v0/spec` (AudioSpec, audio format constants)
**Produces:** `Hit` struct (`Step int`, `Sample string`, `Pan float64`, `Velocity float64`); `PatternProvider` interface (`Next(bar int) (hits []Hit, bpm int, stepsPerBar int, err error)`); `AudioSink` interface (the atomix methods above); `atomixSink` adapter; `Engine` with `Run(ctx, PatternProvider) error` — used by 3.1, 4.1, 4.2, 6.1
**Seams:** audio device boundary — `AudioSink` interface with a real `atomixSink` adapter and a `fakeSink` in the test; the engine is tested against `fakeSink`, never a real device
**Tests:** `TestEngine_SchedulesOnContinuousClock` (bar N+1 start = bar N start + bar duration; fires recorded by `fakeSink` land at `barStart + step*stepDuration`); `TestEngine_HoldsAudioDeviceOpen` (`fakeSink` records exactly one `OpenAudio`, zero `Teardown` across multiple bars); `TestEngine_SigIntTeardown` (ctx cancel → one `Teardown`)
**Model tier:** standard
- [ ] RED: write `TestEngine_SchedulesOnContinuousClock` against a `fakeSink` (engine + provider not yet implemented) — fails to compile
- [ ] Define `Hit`, `PatternProvider`, `AudioSink` in `engine.go`
- [ ] Implement `atomixSink` in `sink.go` wrapping `gopkg.in/mix.v0` (aliased `atomix`)
- [ ] Implement `Engine.Run`: open audio once, loop bar-by-bar on a continuous absolute clock, call `provider.Next(bar)`, schedule `sink.SetFire` per hit at `barStart + step*stepDuration`, honor ctx cancel → `Teardown`
- [ ] GREEN: test passes
- [ ] RED+GREEN: `TestEngine_HoldsAudioDeviceOpen`, `TestEngine_SigIntTeardown`
- [ ] Wave gate: gates green

#### Task 2.2: Real-time pacing (Clock seam)

Added after 2.1's review: `Engine.Run` as delivered by 2.1 has no real-time
pacing — it's an unbounded loop with no wait of any kind between bars, only a
non-blocking `ctx.Done()` check. Proven empirically: against a non-blocking
`PatternProvider`, it scheduled 722,433 bars in 50ms (~14M bars/sec). 2.1's
own tests didn't catch this because their test doubles pace the loop via a
blocking channel (`notify`), which is a test-only artifact. This breaks two
downstream waves: wave 4's continuous playback (100% CPU, unbounded growth in
`mix.v0`'s fire queue instead of audible music) and wave 5's hot reload
("reloads apply only at the next bar boundary" is impossible if every future
bar is already scheduled instantly).

**Files:** Edit `internal/engine/engine.go`, `internal/engine/engine_test.go`
**Write-scope:** `internal/engine/engine.go`, `internal/engine/engine_test.go`
**Consumes:** `Engine`, `AudioSink`, `PatternProvider`, `Hit` from 2.1
**Produces:** `Engine.Run` paces itself against real bar boundaries (waits,
context-aware, for each bar's duration to actually elapse — relative to a
reference time captured at `Run` entry — before calling `provider.Next` for
the next bar) via an injectable clock seam, so tests stay fast/deterministic
without real `time.Sleep` — used by 4.1, 4.2, 5.1
**Seams:** a `Clock` (or equivalent) abstraction — real implementation backs
`Run`'s default construction path (no API break for future callers using
`NewEngine(sink)`); a fake/manual clock in the test drives bar-by-bar
progression without wall-clock waits
**Tests:** existing three (`TestEngine_SchedulesOnContinuousClock`,
`TestEngine_HoldsAudioDeviceOpen`, `TestEngine_SigIntTeardown`) continue to
pass, updated as needed for the new seam; new test proves `Run` does not call
`provider.Next` for bar N+1 until the fake clock has advanced past bar N's
duration, and that ctx cancellation during the wait returns promptly (not
after the full remaining wait)
**Model tier:** standard
- [ ] RED: write the new pacing test against a fake clock — fails (no clock seam exists yet)
- [ ] Add the `Clock` seam; wire a real-clock default into `Engine`'s existing constructor path
- [ ] Implement the context-aware wait in `Run` between bars
- [ ] GREEN: new test passes
- [ ] Update the three existing tests for the new seam; confirm they still pass and still run in well under 1s total (no reliance on real sleeps)
- [ ] Wave gate: gates green

**Wave 2 gate — CLOSED @0a1c320:** `gofmt -l .` clean; `go vet ./...` exit 0;
`go build ./...` exit 0; `go test ./...` exit 0; all 5 engine tests pass in
0.09s. `Engine.Run` demonstrably paces against real time, not just a blocking
test double — re-verified with a real-clock scratch check (1 fire scheduled
in 602ms against a non-blocking provider at 120bpm/16steps, vs. 722,433
fires/50ms before 2.2's fix). Refactor pass: reviewed `engine_test.go` for
duplication (the two drain loops in `TestEngine_HoldsAudioDeviceOpen`/
`TestEngine_SigIntTeardown` are near-identical); left as-is — extracting a
helper for two ~10-line loops isn't worth the indirection at this size.

---

### Wave 3 — Static provider

Spec coverage: spec Wave 1 (StaticProvider reproducing `m001`).

#### Task 3.1: StaticProvider
**Files:** Create `internal/engine/static.go`, `internal/engine/static_test.go`
**Write-scope:** `internal/engine/static.go`, `internal/engine/static_test.go`
**Consumes:** `PatternProvider`, `Hit` from 2.1
**Produces:** `StaticProvider` (implements `PatternProvider`) returning the 16 `m001` hits at bpm 120, 16 steps/bar — used by 4.1
**Seams:** none (pure data)
**Tests:** `TestStaticProvider_ReturnsM001Hits` (16 hits, exact sample/step per `m001.go`); `TestStaticProvider_Tempo` (bpm 120, stepsPerBar 16)
**Model tier:** haiku (mechanical: data transcription from `m001.go`)
- [ ] RED: `TestStaticProvider_ReturnsM001Hits` — fails to compile
- [ ] Transcribe the `m001` pattern from `m001.go` into `StaticProvider.Next`
- [ ] GREEN: test passes
- [ ] RED+GREEN: `TestStaticProvider_Tempo`
- [ ] Wave gate: gates green

**Wave 3 gate:** gates green; static provider tests pass.

---

### Wave 4 — Initial CLI + Fennel provider (parallel)

Spec coverage: spec Wave 1 (runnable player playing `m001` continuously) via 4.1; spec Wave 2 (Fennel provider) via 4.2. No write-scope overlap: 4.1 owns `cmd/basso/`; 4.2 owns `internal/engine/fennel.*` and the vendored compiler.

#### Task 4.1: CLI v1 (Engine + StaticProvider)
**Files:** Create `cmd/basso/main.go`
**Write-scope:** `cmd/basso/main.go`
**Consumes:** `Engine` from 2.1; `StaticProvider` from 3.1
**Produces:** a `basso` binary that plays the `m001` pattern continuously until Ctrl-C — used as the base for 6.1's swap
**Seams:** audio boundary — unit-testable parts are arg parsing/provider wiring (see 6.1 test); real-audio playback is a manual smoke, not automated here
**Tests:** none new (Wave 4 gate smoke covers it)
**Model tier:** standard
- [ ] Write `cmd/basso/main.go`: build `Engine` + `StaticProvider`, call `Engine.Run(ctx)` with SIGINT → ctx cancel
- [ ] Manual smoke: `nix-shell --run 'go run ./cmd/basso'` plays `m001` continuously until Ctrl-C
- [ ] Wave gate: gates green + smoke green

#### Task 4.2: FennelProvider core
**Files:** Create `internal/engine/fennel.go`, `internal/engine/fennel_test.go`, `internal/engine/fennel/compiler.lua` (vendored Fennel compiler)
**Write-scope:** `internal/engine/fennel.go`, `internal/engine/fennel_test.go`, `internal/engine/fennel/compiler.lua`
**Consumes:** `PatternProvider`, `Hit` from 2.1; `github.com/yuin/gopher-lua`
**Produces:** `FennelProvider` constructed by `New(source string) (*FennelProvider, error)` (implements `PatternProvider`); binds `bpm`/`steps` host functions; maps a hit table `{step sample pan? velocity?}` to `Hit` — used by 5.1 and 6.1
**Seams:** interpreter boundary — integration test against the real gopher-lua + vendored Fennel compiler (pure-Go, deterministic). If Fennel-on-gopher-lua is broken here, fall back to plain Lua on the same VM per Global Constraints and record the decision in `memory-progress.md`
**Tests:** `TestFennelProvider_ReproducesM001` (a `.fnl` source returning the 16 `m001` hits yields the same `[]Hit` as `StaticProvider`); `TestFennelProvider_HitDefaults` (omitted `pan` → random-in-[-1,1] sentinel handled by engine; omitted `velocity` → 1.0); `TestFennelProvider_TempoFunctions` (`(bpm 140)` / `(steps 12)` set returned bpm/steps)
**Model tier:** standard
- [ ] Vendor the Fennel compiler as `internal/engine/fennel/compiler.lua` (license-permitted)
- [ ] RED: `TestFennelProvider_ReproducesM001` — fails to compile
- [ ] Implement `FennelProvider.New`: create gopher-lua VM, load the Fennel compiler, compile+eval the source, bind `bpm`/`steps` host functions
- [ ] Implement `Next(bar)`: call the script's `pattern(bar)`, map each hit table to `Hit`
- [ ] GREEN: test passes; if Fennel-on-gopher-lua is broken, switch to plain Lua (same VM) and record decision
- [ ] RED+GREEN: `TestFennelProvider_HitDefaults`, `TestFennelProvider_TempoFunctions`
- [ ] Wave gate: gates green

**Wave 4 gate:** gates green; 4.1 smoke green (real audio, `m001` continuous); Fennel provider tests pass.

---

### Wave 5 — Hot reload

Spec coverage: spec Wave 3 (hot reload, bar-granular, no audio interruption).

#### Task 5.1: FennelProvider + fsnotify, bar-granular reload
**Files:** Modify `internal/engine/fennel.go`; create `internal/engine/fennel_reload_test.go`
**Write-scope:** `internal/engine/fennel.go`, `internal/engine/fennel_reload_test.go`
**Consumes:** `FennelProvider` from 4.2; `github.com/fsnotify/fsnotify`
**Produces:** `FennelProvider.NewFromFile(path string) (*FennelProvider, error)` — starts an fsnotify watcher, buffers a pending source, and applies it on the next `Next(bar)` call (bar-granular) — used by 6.1
**Seams:** fsnotify boundary — test the pending-source apply logic directly via a test hook (`setPendingSource(string)`) for determinism; one `TestFennelProvider_RealFsnotify` smoke against a temp file
**Tests:** `TestFennelProvider_ReloadAtBarBoundary` (set pending source mid-bar; `Next(currentBar)` still returns old hits, `Next(currentBar+1)` returns new hits); `TestFennelProvider_NoAudioRestartOnReload` (the engine never calls `Teardown`/`OpenAudio` between bars — verified at the engine level via `fakeSink` across a reload); `TestFennelProvider_RealFsnotify` (write temp file, assert next bar reflects it)
**Model tier:** standard
- [ ] RED: `TestFennelProvider_ReloadAtBarBoundary` via the `setPendingSource` hook — fails
- [ ] Add pending-source field + apply-on-`Next` logic in `FennelProvider`
- [ ] Add `NewFromFile`: start fsnotify watcher (debounced ~100ms) that writes the new source into pending
- [ ] GREEN: test passes
- [ ] RED+GREEN: `TestFennelProvider_NoAudioRestartOnReload`, `TestFennelProvider_RealFsnotify`
- [ ] Wave gate: gates green

**Wave 5 gate:** gates green; reload tests pass.

---

### Wave 6 — CLI v2 + contract

Spec coverage: spec Wave 4 (CLI ergonomics) + the expand-migrate-contract contract phase for the old code.

#### Task 6.1: CLI v2 (FennelProvider.NewFromFile) + delete old code
**Files:** Modify `cmd/basso/main.go`; create `cmd/basso/main_test.go`; delete `const.go`, `m001.go`
**Write-scope:** `cmd/basso/main.go`, `cmd/basso/main_test.go`, `const.go` (delete), `m001.go` (delete)
**Consumes:** `Engine` from 2.1; `FennelProvider.NewFromFile` from 5.1
**Produces:** `basso play <file.fnl>` (and `basso <file.fnl>` alias) that plays the Fennel file, reloads on save at the next bar, prints bar counter + active bpm, and exits cleanly on Ctrl-C
**Seams:** audio boundary — `TestMain_ArgParsing` covers flag/arg handling and provider construction with a stub provider (no audio); real-audio live-coding loop is a manual smoke
**Tests:** `TestMain_ArgParsing` (`basso play foo.fnl` and `basso foo.fnl` both resolve to file `foo.fnl`); `TestMain_RejectsMissingArg`
**Model tier:** standard
- [ ] RED: `TestMain_ArgParsing` — fails
- [ ] Modify `cmd/basso/main.go`: parse args, build `FennelProvider.NewFromFile`, run `Engine`, print bar counter + bpm, SIGINT → cancel
- [ ] GREEN: test passes
- [ ] Contract: delete `const.go` and `m001.go`
- [ ] Contract acceptance: `grep -R "func play" --include='*.go' .` returns nothing; `grep -R "var pattern" --include='*.go' .` returns nothing; `go build ./...` succeeds (only `cmd/basso` entry remains)
- [ ] Manual smoke: edit a `.fnl`, save, hear the change on the next bar, Ctrl-C exits cleanly (real audio device — a skip is not a pass)
- [ ] Wave gate: gates green + smoke green

**Wave 6 gate:** gates green; CLI tests pass; contract grep clean; end-to-end smoke green (real audio: edit-save-hear-next-bar, clean Ctrl-C).

---

## Self-review (run before dispatch)

- **Spec coverage**: spec Goal/Design/Engine loop → 2.1, 5.1; PatternProvider → 2.1; Script API (Fennel) → 4.2; Hot reload → 5.1; CLI → 4.1, 6.1; Non-goals/Constraints → Global Constraints block; spec Waves 0–4 → plan Waves 1, (2+3+4.1), 4.2, 5, 6. Every spec section maps to ≥1 task. ✓
- **Placeholder scan**: no TBD / "handle edge cases" / "similar to task N". Edge cases named as specific tests (reload at bar boundary, no audio restart, hit defaults). ✓
- **Name/type consistency**: `Hit`/`PatternProvider` (2.1) consumed identically by 3.1, 4.2; `Engine` (2.1) consumed by 4.1, 6.1; `StaticProvider` (3.1) consumed by 4.1; `FennelProvider.New` (4.2) consumed by 5.1; `FennelProvider.NewFromFile` (5.1) consumed by 6.1. Produces/Consumes match exactly. ✓
- **Write-scope overlaps**: none within a wave. Wave 4 splits cleanly: 4.1 → `cmd/basso/`, 4.2 → `internal/engine/fennel.*` + vendored compiler. ✓