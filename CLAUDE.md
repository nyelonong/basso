# CLAUDE.md

This file provides guidance to Claude Code (claude.ai/code) when working with code in this repository.

## What this is

`basso` is being built into a **live-coding player**: a persistent process that
plays a Fennel pattern continuously and reloads it at the next bar boundary when
the source file is saved, with no audio restart. Audio is triggered through
`gopkg.in/mix.v0` (the successor to `github.com/outrightmental/go-atomix`; source
repo at `github.com/go-mix/mix`, which declares its module path as
`gopkg.in/mix.v0`), imported aliased as `atomix`. Patterns are Fennel (Lisp)
scripts evaluated by an embedded gopher-lua interpreter.

> **Status: mid-rebuild.** Spec: `docs/specs/2026-07-19-live-coding-player.md`
> (lifecycle: in-progress). Plan: `docs/plans/2026-07-19-live-coding-player.md`.
> The legacy `const.go` / `m001.go` play-once engine has been removed (early
> contract); the new `cmd/basso` entry and `internal/engine` packages are being
> built per the plan. There is no runnable binary yet.

## Building

- Module: `github.com/nyelonong/basso`.
- Enter the devshell: `nix-shell` (provides SDL2, PortAudio, sox, pkg-config, Go —
  see `shell.nix`). **Do not use Homebrew.**
- Build: `nix-shell --run 'go build ./...'`
- Gates: `gofmt -l .`, `go vet ./...`, `go test ./...` (see `docs/agents/galdr.md`).
- Runnable `basso` binary lands in plan wave 4 (CLI v1) / wave 6 (CLI v2).

## System dependencies

Provided by the nix devshell (`shell.nix`), not brew: SDL2, PortAudio, sox
(libsox — the `gopkg.in/mix.v0` bind package transitively imports `go-sox`, a
cgo dep), pkg-config, and Go. The audio library uses cgo via these.

## Architecture (target)

- `cmd/basso/main.go` — CLI entry (`basso play <file.fnl>`), built in waves 4/6.
- `internal/engine/` — the persistent bar-loop `Engine`; `Hit` /
  `PatternProvider` / `AudioSink` types; providers `StaticProvider`
  (regression fixture holding the `m001` pattern as data) and `FennelProvider`
  (gopher-lua + vendored Fennel compiler; owns bar-granular hot reload via
  fsnotify). `AudioSink` is a seam with a real `atomixSink` (wrapping
  `gopkg.in/mix.v0`) and a `fakeSink` for tests.
- `sound/808/` — WAV samples, loaded by `gopkg.in/mix.v0` via
  `atomix.SetSoundsPath`.
- Timing: `stepDuration = time.Minute / (bpm*4)` → sixteenth-note resolution; 16
  steps = one bar. The engine re-evaluates the Fennel script and calls
  `pattern(bar)` once per bar boundary; reloads apply only at that boundary.

## Conventions

- Hot reload is bar-granular: pattern changes take effect at the next bar
  boundary, never mid-bar. The audio device is opened once at startup and held
  open across reloads.
- Pure-Go app code only (no cgo in app code); the native audio libs come from
  the nix devshell.

<!-- galdr:start -->
## galdr

Per-repo galdr config lives at [`docs/agents/galdr.md`](docs/agents/galdr.md) —
gates, invariants, models, worktree notes, smoke, and budget thresholds. Read
that file for the authoritative command list; this block only points to it.
<!-- galdr:end -->