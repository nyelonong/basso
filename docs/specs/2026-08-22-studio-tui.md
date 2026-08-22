# Studio TUI — status and candidate reviewer

Supersedes the "No GUI, TUI" scope line in
`docs/specs/2026-07-25-transactional-ai-suggestions.md` for this surface only.

## Goal

`basso studio <file.fnl>` opens a full-screen terminal UI that shows what the
engine is doing (bar, BPM, steps, reload events) and drives the AI-suggestion
loop in one place: trigger a suggestion, watch local validation, read the
candidate diff, apply or reject with one keypress. Playback behavior —
bar-granular hot reload, open audio device, transactional candidates — is
unchanged. The user keeps editing Fennel patterns in their own editor.
`studio` accepts the suggestion flags (`--provider`, `--model`, `--timeout`,
`--sounds`) with the same defaults and environment variables as `suggest`,
because it hosts that flow.

Success looks like: start `basso studio patterns/basic-groove.fnl`, see the bar
counter advance, press `s`, watch the provider propose a candidate and
validation pass or fail, read the diff, press `a` to apply it to the source
file and hear the change at the next bar boundary.

## Non-goals

- No pattern editing inside the TUI; the Fennel source stays edited externally.
- No step-grid sequencer, mouse-drawn patterns, sample browsing, or waveforms.
- No background daemon: quitting `studio` stops playback, same as `play`.
- No changes to the engine's reload semantics or audio path.
- No new AI transport behavior: providers, repair loop, and validation are the
  existing `internal/ai` and `internal/suggest` code paths.
- `basso play`, `suggest`, `apply`, and `help` keep their current output and
  contracts exactly.

## Constraints

- Pure Go, `CGO_ENABLED=0` clean. Dependencies limited to Bubble Tea
  (`bubbletea`), `bubbles`, and `lipgloss`; all pure Go.
- Playback stays local and offline. Only the suggest action may call a
  configured AI provider; proposals remain untrusted until local validation
  succeeds.
- Applies go through the existing candidate store and transactional applier
  with backups; the TUI adds no second write path to pattern sources.
- Engine gains at most a minimal read-only status seam (current bar, BPM,
  steps, reload/activation events) consumed by both `play`'s line output and
  `studio`; no engine internals are exported beyond that seam.
- The suggest request runs as an asynchronous Bubble Tea command: the UI stays
  responsive, shows progress, and remains cancellable while a provider call is
  in flight.
- Gates stay green: `gofmt -l .`, `go vet ./...`, `go test ./...`.

## Decision log

| Decision | Choice | Reason |
| --- | --- | --- |
| UI form | TUI (Bubble Tea), not GUI | Terminal-native workflow, diff-centric AI flow, preserves pure-Go/cgo-free build |
| Scope | Status + candidate reviewer | Complements the external-editor live-coding loop; editing belongs to the editor |
| Entry point | New `basso studio <file.fnl>` subcommand | Zero churn on the tested `play` contract; promote to default later if it earns it |
| Quit semantics | Stop playback on quit | Matches `play`; the spec for suggestions forbids daemons |
| Candidate write path | Existing store + applier only | One transactional write path; TUI is presentation |

## Acceptance criteria

1. `basso studio <file.fnl>` plays the pattern and advances a visible bar
   counter with current BPM and steps; saving an edit hot-reloads at the next
   bar boundary with an on-screen reload event and no audio restart.
2. Pressing the suggest key prompts for an editable request, calls the
   configured provider asynchronously, and shows a progress state until the
   proposal returns; cancelling keeps playback running.
3. A returned candidate renders as a unified diff against the active source
   with its validation status before any key can modify files.
4. Apply writes through the existing applier (backup created, candidate marked
   applied); reject discards without touching the source file. Both update the
   review pane state.
5. Invalid model output, failed validation, and missing or invalid provider
   configuration each render actionable failure diagnostics in the UI;
   playback continues uninterrupted in every error path.
6. `play`, `suggest`, `apply`, and `help` outputs are byte-identical to today;
   their tests pass unchanged except where help text legitimately grows by one
   usage line for `studio`.
7. `CGO_ENABLED=0 go build ./...` succeeds and `make gates` passes.
