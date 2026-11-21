---
name: basso
description: Write, check, render, measure and play beats with basso, a live-coding beat player whose patterns are Fennel files. Use when the user asks to make a beat, groove, loop or track in a style (funkot, Indo bounce, breakbeat, house, and so on), to fix how a basso pattern sounds, to export it as a WAV, or to play it for them.
---

# basso

basso plays a pattern file bar by bar. A pattern is a Fennel (Lisp) file that
returns a function; basso calls it once per bar with the bar number and plays
the hits it returns. The CLI runs the same engine as the website
(basso.discoverd.io), including its master limiter.

## Setup

Run once in a checkout of the basso repository:

```sh
cd site && npm install
```

Run the CLI from any directory. File paths are relative to that directory:

```sh
node <repo>/site/cli/basso.mjs <command> ...
```

## Commands

| Command | What it does | Exit code |
|---|---|---|
| `check FILE [--bars N] [--json]` | Evaluates bars 0 to N-1 (default 16) and validates every hit. Prints a summary or the first error with its line and bar. | 0 ok, 1 error |
| `render FILE [--bars N] [--out F.wav] [--json]` | Renders N bars (default 8) offline, prints measurements, and writes a 16-bit stereo WAV when `--out` is given. | 0 ok, 1 error |
| `play FILE` | Plays through the speakers. Saving the file applies the change at the next bar. A broken save is reported once and the last good pattern keeps playing. Stop it with Ctrl+C. | 0 after Ctrl+C, 1 if the file fails before playback starts |
| `samples [--json]` | Lists the sample names and synth instruments. | 0 |

Use `--json` when you read the output yourself. `play` runs until it is
stopped, so start it in a separate terminal or background process, and stop
it before you start another one.

## Pattern format

```fennel
(bpm 135)      ; whole number, 20 to 400; default 120
(steps 16)     ; steps per bar, 1 to 256; default 16 (sixteenth notes)
(swing 54)     ; 50 (straight, default) to 75; delays every odd step

(fn pattern [bar]
  (stack
    (seq "kick_hard.wav" "x... x... x... x...")
    (seq "cl_hihat.wav"  "xoxo xoxo xoxo xoxo" {:velocity 0.5 :pan -0.2})
    (melody "bass" ". . A1 _ . . A1 _ . . A1 _ . . A1 _")))

pattern        ; the last form must be the pattern function
```

`bar` starts at 0 and counts up forever. Use it for arrangement, for example
`(% bar 8)` for the position in an 8-bar phrase, `(= (% bar 4) 3)` for a fill
bar, and a table lookup such as `(. roots (+ 1 (% bar 4)))` for a chord
progression.

### Short notation

- `(seq SAMPLE STEPS [OPTIONS])`: one character per step. `x` is a hit at
  velocity 1, `o` is a soft ghost hit at velocity 0.5, `.` or `-` is a rest.
  Spaces, tabs, newlines and `|` are ignored, so group steps in fours.
- `(melody INSTRUMENT TOKENS [OPTIONS])`: tokens separated by whitespace or
  `|`, one per step. A note name plays, `_` holds the previous note one more step, `.` or
  `-` rests.
- `(stack A B ...)`: joins lists of hits and single hit tables into one list.
  It skips `nil`, so `(when fill? (seq ...))` is safe.
- OPTIONS is a table copied onto every hit, such as `{:pan 0 :velocity 0.6}`.
  Its `:velocity` multiplies the step velocity. `seq` options cannot set
  `:step`, `:sample` or `:note`; `melody` options cannot set those,
  `:instrument` or `:length` (use `_`).

### Hit tables

The helpers produce these. You can also write them directly:

| Field | Meaning | Rule |
|---|---|---|
| `:step` | Step in the bar | Required. 0 to steps-1 |
| `:sample` | Sample file name | Exactly one of `:sample` or `:note`. Must be in `samples` |
| `:note` | Pitch such as `"C2"`, `"A#1"`, `"Eb3"` | Exactly one of `:sample` or `:note` |
| `:instrument` | `bass`, `brass`, `pluck`, `lead`, `pad` | Notes only. Default `bass` |
| `:length` | Note length in steps | Notes only. 1 to 4096. Default 1 |
| `:velocity` | Loudness | 0 to 1. Default 1 |
| `:pan` | Left -1 to right 1 | Default 0 for notes, random for samples |

Limits per bar: at most 4096 hits. `lead` and `pad` notes must end within
their bar, with at most 64 such hits and 8 sounding at once.

The Lua standard library is available except `os`, `io`, `debug`, `package`,
`require`, `dofile`, `loadfile` and `coroutine`. Write code that works in both
Lua 5.1 and 5.4: use `(table.insert t x)`, not `table.unpack`.

### Sounds

Run `samples` for the exact list. A hit names the file with its extension,
such as `"kick_hard.wav"`. The kit has the classic 808 samples plus
`kick_hard`, `snare_hard`, `kendang_dung`, `kendang_tung`, `kendang_tak`,
`siren`, `hoover` (fixed pitch A), `stab` (A minor chord), `rev_cymbal`
(a one-bar reverse swell at 180 BPM; start it on step 0 of the bar before a
downbeat) and `zap`. Samples play at a fixed pitch; use `melody` for anything
tuned.

## Workflow

1. Write the pattern file.
2. Run `check FILE --json`. Fix every error before you continue. The error
   names the line, the bar and the rule.
3. Run `render FILE --bars N --json` with N covering a full phrase (8 or 16).
4. Read the measurements and change the pattern (see the next section). Repeat
   steps 2 to 4 until there are no warnings and the per-bar levels show the
   arrangement you intended.
5. When the user wants to hear it, start `play FILE`, or give them the WAV
   from `--out`.

## Reading the measurements

All levels are dB relative to full scale; 0 is the loudest possible.

- `peakDb`: the loudest sample. About -1 to -3 is normal; the master limiter
  keeps it under 0.
- `rmsDb`: average loudness. Dense dance music sits around -8 to -11.
- `clippedSamples`: samples at full scale. Must be 0; any clipping is a
  warning.
- `limitedPercent`: samples the limiter had to bend. Above 1% the mix is too
  hot: lower the velocities of the loudest parts, usually kick and bass.
- `bands`: `lowDb` under 200 Hz (kick, bass), `midDb` from 200 Hz to 4 kHz
  (snare, claps, leads, chords), `highDb` above 4 kHz (hats, shakers,
  cymbals). Lows 10 to 18 dB above highs is normal for dance music.
- `bars`: the same levels for each bar. Use them to check arrangement: a drop
  or chorus should be at least 2 to 3 dB higher in `midDb` or `highDb` than
  the bars before it, and a build should rise bar by bar. Total `rmsDb` hides
  this because kick and bass dominate it; compare the mid and high columns.
- `warnings`: specific problems with a suggested fix. Fix them.

The measurements check balance, level and arrangement. They cannot tell you
whether a groove feels right or fits a style. Ask the user to listen and say
so when you report. The CLI's audio engine can differ from a browser's by
about 1 dB in level.

## Style starting points

| Style | BPM | Swing | Shape |
|---|---|---|---|
| Funkot | 170 to 190 | 50 | Kick on every beat, open hats on off-beats, off-beat octave bass, kendang and tom rolls into each phrase |
| Indo bounce (jedag-jedug) | 128 to 140 | 50 | Hard four-on-the-floor kick, off-beat bass, bouncy lead riff in the drop, snare roll builds |
| Breakbeat | 130 to 140 | 52 to 56 | Syncopated kick and snare with ghost notes (`o`), rolling bass, chopped fills every 4th bar |
| House | 120 to 128 | 50 to 58 | Kick on every beat, clap on 2 and 4, off-beat open hat, chord stabs |

Full examples are in `patterns/` in the repository: `jedag-jedug.fnl`,
`breakbeat.fnl`, `shuffle-groove.fnl` and `indo-bounce.fnl`.
