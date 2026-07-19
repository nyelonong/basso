# CLAUDE.md

This file provides guidance to Claude Code (claude.ai/code) when working with code in this repository.

## What this is

`basso` is a **live-coding player**: a persistent process that plays a
Fennel pattern continuously and reloads it at the next bar boundary when the
source file is saved, with no audio restart. Audio is played through
`github.com/gopxl/beep/v2` (`speaker`/`wav`/`effects` — built on
`ebitengine/oto` for real device output). Patterns are Fennel (Lisp)
scripts evaluated by an embedded gopher-lua interpreter.

> **Status: done.** Spec: `docs/specs/2026-07-19-live-coding-player.md`
> (lifecycle: done). Plan: `docs/plans/2026-07-19-live-coding-player.md`
> (all 8 tasks across 6 waves complete). `basso play <file.fnl>` (alias:
> `basso <file.fnl>`) is a runnable binary.

## Building

Plain Go, no devshell needed — a standard Go toolchain matching `go.mod`'s
`go 1.26.5` directive is all that's required.

- Module: `github.com/nyelonong/basso`.
- Build: `go build ./...` (or `make build`).
- Install: `make install` — `go install ./cmd/basso`, then `basso play
  <file.fnl>` works directly.
- Gates: `gofmt -l .`, `go vet ./...`, `go test ./...` (see
  `docs/agents/galdr.md`; or `make gates`).
- Run: `go run ./cmd/basso play <file.fnl>` (or `make run FILE=<file.fnl>`).

## System dependencies

None. The build is pure Go, `CGO_ENABLED=0` clean — confirmed by building
and running with it explicitly set, and outside any devshell. `github.com/
gopxl/beep/v2`'s device backend (`ebitengine/oto` via `ebitengine/purego`)
talks to the OS audio API directly via dlopen-based FFI, no cgo, no native
libs. This project used to need a nix devshell (`shell.nix`, now removed)
for SDL2/PortAudio/sox/pkg-config, from the pre-rebuild `go-atomix`/early
`gopkg.in/mix.v0` era — dead weight once the `gopxl/beep/v2` swap landed,
confirmed and removed.

## Architecture

- `cmd/basso/main.go` — CLI entry (`basso play <file.fnl>`, alias `basso <file.fnl>`).
- `internal/engine/` — the persistent bar-loop `Engine`; `Hit` /
  `PatternProvider` / `AudioSink` types; providers `StaticProvider`
  (regression fixture holding the `m001` pattern as data) and `FennelProvider`
  (gopher-lua + vendored Fennel compiler; owns bar-granular hot reload via
  fsnotify). `AudioSink` is a seam with a real `beepSink` (wrapping
  `github.com/gopxl/beep/v2`) and a `fakeSink` for tests.
- `sound/808/` — WAV samples, decoded via `beep/v2/wav.Decode` and cached
  per sample name as `*beep.Buffer` by `beepSink`.
- Timing: `stepDuration = time.Minute / (bpm*4)` → sixteenth-note resolution; 16
  steps = one bar. The engine re-evaluates the Fennel script and calls
  `pattern(bar)` once per bar boundary; reloads apply only at that boundary.

## Conventions

- Hot reload is bar-granular: pattern changes take effect at the next bar
  boundary, never mid-bar. The audio device is opened once at startup and held
  open across reloads.
- Pure-Go, no cgo, anywhere in the dependency tree (`CGO_ENABLED=0` builds
  and runs correctly) — `gopxl/beep/v2`'s audio backend uses dlopen-based FFI
  (`ebitengine/purego`) instead of cgo bindings to native libs.

<!-- galdr:start -->
## galdr

Per-repo galdr config lives at [`docs/agents/galdr.md`](docs/agents/galdr.md) —
gates, invariants, models, worktree notes, smoke, and budget thresholds. Read
that file for the authoritative command list; this block only points to it.
<!-- galdr:end -->