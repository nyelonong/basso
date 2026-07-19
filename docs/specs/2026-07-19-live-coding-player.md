# Spec: basso live-coding player

Lifecycle status: done

## Goal

Turn basso from a play-once-and-exit drum loop (`go run const.go m001.go`) into a
**live-coding player**: a persistent process that plays a pattern continuously
while you edit the pattern's source in your text editor. On save, the new pattern
takes effect at the next bar boundary with no audio restart. Patterns are written
in Fennel (a Lisp) running on an embedded gopher-lua interpreter; the engine
re-evaluates the script each bar and schedules the returned hits through the
existing `go-mix/mix` sample-trigger engine. "Music from code" becomes: write
Fennel, save, hear it on the next bar, edit again.

## Non-goals

- ~~**No synthesis.**~~ Superseded post-v1: `:instrument` (see Script API)
  picks a synthesized voice — `"bass"`/`"brass"` (sawtooth, per-voice fixed
  attack/release envelope) or `"pluck"` (Karplus-Strong string synthesis) —
  alongside WAV sample triggering. Still out of scope beyond that: no
  filters/EQ, no user-configurable envelope/decay parameters, no per-note
  effects chains, no piano/sample-based or FM-synthesis voices.
- **No Tidal-style time combinators or Sonic-Pi-style imperative `live_loop`.**
  The script↔engine contract is the declarative bar function (Decision 4).
- **No GUI / web editor.** Editing happens in the user's text editor; basso only
  watches the file and plays.
- **No MIDI, recording, audio export, or multi-output routing** in v1.
- **No host rewrite.** The engine stays Go; Rust is out of scope (Rust is
  compiled and cannot be re-evaluated per bar, so it does not fit the live-coding
  model as a pattern language).

## Constraints

Project-wide rules every task must obey. Copied forward verbatim by the plan
skill; written as rules.

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

## Decision log

| #  | Decision | Choice |
|----|----------|--------|
| 1  | What basso becomes | Live-coding player (edit pattern while audio plays; changes apply next bar) |
| 2  | How edited patterns reach the engine | Embedded script interpreter; engine re-evaluates the script each bar |
| 3  | Pattern script language | Fennel (Lisp) over gopher-lua (pure Go, in-process, no cgo) |
| 4  | Script↔engine contract | Declarative bar function `pattern(bar)` returning a list of hits |
| 5  | Edit delivery (assumed) | fsnotify file-watch, debounced; swap source at the bar boundary |
| 6  | Tempo source (assumed) | `bpm` set in the script; default 120; default 16 steps/bar (sixteenth notes) |
| 7  | Audio device lifetime (assumed) | opened once at startup, held open across reloads |
| 8  | Host language (assumed) | Go (not rewritten in Rust) |
| 9  | Fennel fallback (assumed) | plain Lua on the same gopher-lua VM |

## Design

### Engine loop

The engine replaces `const.go`'s `play()` (which loops N times then exits) with a
persistent loop:

1. Open the audio device once (`atomix.Configure` + `atomix.OpenAudio`), set
   `atomix.SetSoundsPath("sound/808/")`, start at a 1-second lead as today.
2. Loop forever, one bar at a time. Before each bar:
   a. If a new pattern source is pending (file changed), re-compile + re-eval the
      Fennel script and update the active `pattern` function and tempo.
   b. Call `pattern(barIndex)` to get the hits for this bar.
   c. For each hit, schedule `atomix.SetFire(sample, absTime, 0, velocity, pan)`
      at `barStart + step*stepDuration`.
3. Advance `barIndex`, wait until the bar elapses, repeat.
4. On SIGINT, `atomix.Teardown()` and exit.

`stepDuration = time.Minute / (bpm * 4)` (sixteenth-note resolution), matching
the current code. `barStart` is computed on a continuous absolute clock so bars
stay phase-locked across reloads.

### Pattern provider interface

The engine reads hits through a Go interface so the source is swappable:

```go
type Hit struct {
    Step      int       // 0..stepsPerBar-1
    Sample    string    // WAV filename in sound/808/, e.g. "kick2"
    Pan       float64   // -1..1; use random in [-1,1] if unset
    Velocity  float64   // 0..1; default 1.0
}

type PatternProvider interface {
    // Next returns the hits for the given bar, and the tempo/steps to use.
    Next(bar int) (hits []Hit, bpm int, stepsPerBar int, err error)
}
```

Wave 1 ships a `StaticProvider` returning the current `m001` pattern (regression
safety). Wave 2 ships the `FennelProvider` implementing this interface.

### Script API (Fennel)

The `.fnl` script exposes a `pattern` function and may call host-provided
functions to configure tempo:

```fennel
(bpm 120)            ; optional; default 120
(steps 16)           ; optional; default 16

(fn pattern [bar]
  ;; bar is the 0-based bar index; loops/conditionals/math all work here
  [{:step 0 :sample "kick2"}
   {:step 2 :sample "maracas"}
   {:step 4 :sample "snare" :pan 0.0}
   {:step 4 :sample "maracas"}
   {:step 7 :sample "kick2" :velocity 0.8}
   {:step 0 :note "C2" :length 4}
   {:step 8 :note "E3" :length 2 :instrument "brass"}
   {:step 12 :note "G2" :length 4 :instrument "pluck"}])
```

- A hit is a table with keys `step` (required) and optional `pan` (-1..1) and
  `velocity` (0..1, default 1.0). `pan` omitted → random in [-1,1], matching
  today's per-fire random pan. Exactly one of `sample` or `note` is also
  required:
  - `sample` — a WAV filename, played as-is (a drum hit).
  - `note` — a synthesized tone at that pitch, in scientific pitch notation
    (e.g. `"C2"`, `"A#1"`); sustains for `length` steps (default 1 if
    omitted). `length` is meaningless on a `sample` hit.
  - `instrument` — which synthesized voice plays a `note` hit: `"bass"`
    (default if omitted), `"brass"`, or `"pluck"`. Meaningless on a `sample`
    hit. Not validated at mapping time — an unrecognized name is caught at
    play time (logged to stderr, hit silently skipped), same as a malformed
    `note`.
  - Both or neither of `sample`/`note` on one hit is a mapping error.
- `pattern` returns a sequence of these tables for the bar.
- `bpm` and `steps` are host functions the engine binds into the Fennel
  environment; the engine uses the last values set during eval for the next bar.

### Hot reload

fsnotify watches the `.fnl` file. Writes are debounced (e.g. 100 ms). On a
debounced write, the new source is read into a string the engine holds. At the
next bar boundary, the engine re-compiles + re-evaluates and swaps the active
`pattern` function. Audio is never interrupted; no `Teardown`/`OpenAudio` between
bars.

### CLI

`basso play <file.fnl>` (alias: `basso <file.fnl>`). Watches the file, plays
continuously, prints a bar counter + the active bpm to stdout. Ctrl-C tears down
cleanly.

## Acceptance criteria (per wave)

### Wave 0 — Project bootstrap

- `go.mod` exists: module `github.com/nyelonong/basso`.
- Deps fetched: `gopkg.in/mix.v0`, `github.com/yuin/gopher-lua`,
  `github.com/fsnotify/fsnotify`, and the Fennel compiler (vendored or fetched).
- A nix devshell (`shell.nix` or flake) provides SDL2 + PortAudio so
  `nix-shell --run 'go build ./...'` works without brew.
- galdr gates green: `gofmt -l .` clean, `go vet ./...` clean, `go test ./...`
  passes (even if only trivial tests exist yet).

### Wave 1 — Persistent engine + bar loop

- `const.go`'s `play()` is replaced by a persistent engine that opens audio once
  and advances bar-by-bar without exiting.
- `PatternProvider` interface + `Hit` struct exist; a `StaticProvider`
  reproduces the current `m001` pattern.
- `basso` plays the `m001` pattern continuously until Ctrl-C.
- Test: `StaticProvider.Next(bar)` returns the expected 16 hits for `m001`; a
  loop test asserts the engine schedules fires on a continuous absolute clock
  (bar N+1 start = bar N start + bar duration).
- galdr gates green.

### Wave 2 — Fennel interpreter as the pattern provider

- `FennelProvider` compiles + evaluates a `.fnl` source via gopher-lua (Fennel
  compiler loaded into the VM) and implements `PatternProvider`.
- Hit tables map to the `Hit` struct exactly as specified (step, sample, pan,
  velocity; defaults for omitted fields).
- A `.fnl` file reproducing the `m001` pattern plays identically to Wave 1's
  `StaticProvider`.
- If Fennel-on-gopher-lua is found broken here, switch the provider to plain Lua
  on the same VM per Constraint 5, and record the decision in memory-progress.
- Test: a sample `.fnl` compiles and `Next(0)` returns the expected hits;
  `bpm`/`steps` host functions set the returned tempo.
- galdr gates green.

### Wave 3 — Hot reload

- fsnotify watches the `.fnl` file; debounced writes swap the source; the next
  bar uses the new pattern with no audio gap.
- Test: simulate a file change (swap source string) and assert `Next(bar+1)`
  reflects the new pattern; assert no `Teardown`/`OpenAudio` occurs between bars
  (audio device held open).
- galdr gates green.

### Wave 4 — Live-coding ergonomics

- CLI `basso play <file.fnl>` works end to end in the nix devshell: edit the
  `.fnl`, save, hear the change on the next bar, Ctrl-C exits cleanly.
- stdout prints a bar counter and active bpm.
- Smoke run green (real audio device) — a skip is not a pass.
- galdr gates green.