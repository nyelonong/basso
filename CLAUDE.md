# CLAUDE.md

This file provides guidance to Claude Code (claude.ai/code) when working with code in this repository.

## What this is

`basso` is a tiny Go program that plays drum-machine patterns from WAV samples using the `github.com/outrightmental/go-atomix` audio library. A "song" is a Go source file containing a pattern; running it plays the loop out the speakers.

## Running

There is no `go.mod` and no build system — the program is run by passing source files explicitly to `go run`:

```
go run const.go m001.go
```

`const.go` holds the shared asset constants and the `play(...)` engine. A song file (e.g. `m001.go`) defines `pattern`, `main()`, and calls `play`. To add a new song, create a new `mNNN.go` with its own `main` and run `go run const.go <newfile>.go`. Never put two `main` packages' `main` functions in the same `go run` invocation.

## System dependencies (required before it will build/run)

- SDL2: `sudo apt-get install libsdl2-dev`
- PortAudio + friends: `sudo apt-get install portaudio19-dev libjack-jackd2-dev libmpg123-dev`
- Go atomix: `go get github.com/outrightmental/go-atomix`

The platform is darwin in this checkout but the README's install commands are Debian/Ubuntu. On this machine, get the system libraries through **nix**, not Homebrew — e.g. `nix-shell -p SDL2 portaudio --run 'go run const.go m001.go'`, or a `shell.nix`/flake devshell.

## Architecture

All files are `package main` in the repo root — there are no subpackages. The split is by role, not by package:

- **`const.go`** — engine. Defines the sample-path/asset-name constants (`path`, `kick1`, `snare`, ...), the audio spec (`sampleHz = 48000`, stereo F32), and `play(pattern []string, loops int, step time.Duration)`. `play` schedules each pattern step via `atomix.SetFire(...)` at absolute times computed from `step`, opens the audio device, then blocks until `atomix.FireCount()` drains. Pan position is randomized per hit.
- **`mNNN.go`** — a song: a `pattern` (slice of the constant names from `const.go`) and a `main` that sets bpm/loops and computes `step = time.Minute / (bpm*4)` (sixteenth-note resolution) before calling `play`.

The WAV samples live in `sound/808/` and are loaded by `go-atomix` from the path set by `atomix.SetSoundsPath(path)`. Pattern entries are string constants that must match WAV filenames in that directory.

## Conventions

- A pattern step duration of `time.Minute / (bpm*4)` means each slice index is one sixteenth note; a 16-entry pattern is one bar at the given bpm.
- `atomix.StartAt` plus a 1-second lead padding is intentional — keep it so the audio engine is ready before the first fire.

<!-- galdr:start -->
## galdr

Per-repo galdr config lives at [`docs/agents/galdr.md`](docs/agents/galdr.md) —
gates, invariants, models, worktree notes, smoke, and budget thresholds. Read
that file for the authoritative command list; this block only points to it.
<!-- galdr:end -->