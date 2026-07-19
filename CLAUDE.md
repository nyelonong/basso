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

- Module: `github.com/nyelonong/basso`.
- Enter the devshell: `nix-shell` (provides SDL2, PortAudio, sox, pkg-config, Go —
  see `shell.nix`). **Do not use Homebrew.**
- Build: `nix-shell --run 'go build ./...'`
- Gates: `gofmt -l .`, `go vet ./...`, `go test ./...` (see `docs/agents/galdr.md`).
- Run: `nix-shell --run 'go run ./cmd/basso play <file.fnl>'`.

## System dependencies

Provided by the nix devshell (`shell.nix`), not brew: SDL2, PortAudio, sox,
pkg-config, and Go. `github.com/gopxl/beep/v2`'s device backend
(`ebitengine/oto`) talks to the OS audio API directly via
`ebitengine/purego` (dlopen-based FFI, no cgo) rather than through
SDL2/PortAudio/sox — those devshell entries predate this backend swap and
are left as-is for now; whether they're still needed is a separate decision
for the controller/user.

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
- Pure-Go app code only (no cgo in app code); the native audio libs come from
  the nix devshell.

<!-- galdr:start -->
## galdr

Per-repo galdr config lives at [`docs/agents/galdr.md`](docs/agents/galdr.md) —
gates, invariants, models, worktree notes, smoke, and budget thresholds. Read
that file for the authoritative command list; this block only points to it.
<!-- galdr:end -->