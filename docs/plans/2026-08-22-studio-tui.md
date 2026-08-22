# Plan: basso studio TUI

Implements `docs/specs/2026-08-22-studio-tui.md`.

next: Task 5

## Architecture

- `cmd/basso/main.go` already decorates the pattern provider (`progressProvider`)
  to print bar/BPM lines, and prints reload diagnostics in its run loop. The
  status seam generalizes these two decorations into observer callbacks; Engine
  and providers stay untouched.
- `basso studio` reuses the exact command wiring seams `play` and `suggest`
  use today (`newProvider`, `newSink`, `newModel`, `newPreflighter`, store
  root), so every factory stays fake-able in tests.
- The TUI layer is a Bubble Tea model fed by the observers and by
  `suggest.NewService`; applies go through `suggest.NewApplier` only.
- New dependency: `github.com/charmbracelet/bubbletea`, `bubbles`, `lipgloss`
  (pure Go).

## Global Constraints

Verbatim from the spec's Constraints section: pure Go / `CGO_ENABLED=0`;
playback local, only suggest calls a provider; proposals untrusted until local
validation succeeds; single transactional write path for candidates; read-only
status seam only; suggest runs as a cancellable async UI command; gates green.

## Tasks

### Task 1 — Status observation seam

Depends on: nothing.

Generalize `progressProvider` and the diagnostic printing into observer
callbacks (bar/bpm/steps events, reload/activation diagnostics). `play` keeps
byte-identical stdout by passing printer callbacks. No behavior change beyond
the refactor.

Evidence: `go test ./cmd/basso/ ./internal/engine/` passes with existing output
assertions unchanged; new test proves an observer receives bar and diagnostic
events.

### Task 2 — Studio command skeleton

Depends on: 1.

Add `basso studio <file.fnl>` with suggestion-flag parity
(`--provider/--model/--timeout/--sounds`, env defaults), help text + usage,
launching a Bubble Tea program wired to playback through the shared factories;
quit stops playback exactly like `play`'s interrupt path.

Evidence: help-text test asserts the new usage line; a headless test starts
studio with a fake sink/provider, quits, and asserts clean teardown with no
writes; `make gates` green.

### Task 3 — Live status pane

Depends on: 1, 2.

Render advancing bar/BPM/steps and a scrolling reload-event log from the Task 1
observers; hot-reload shows an event line and playback continues (no audio
restart path exists to break).

Evidence: Bubble Tea model Update/View tests covering bar advance and reload
event rendering; manual `make run`-style smoke of `studio` on
`patterns/basic-groove.fnl` with a save-triggered reload.

### Task 4 — Suggest action with async provider call

Depends on: 2.

Suggest key opens an editable prompt; submission runs `suggest.Service`
asynchronously with a visible in-flight state and cancellation; every failure
path — provider config missing, HTTP failure, invalid proposal, failed
validation — renders diagnostics while playback continues.

Evidence: model tests with a fake model covering success, provider error, and
config-error paths; cancellation test asserts no state mutation after cancel.

### Task 5 — Candidate review pane

Depends on: 4.

Returned candidates render as a unified diff with validation status; `a` applies
through the existing store + applier (backup written, candidate marked applied);
`r` rejects without touching the source. Review keys are inert until a candidate
exists.

Evidence: model tests over a temp-dir store asserting apply produces backup +
applied state and reject leaves the source untouched; `make gates` green.

### Task 6 — Integration evidence and docs

Depends on: 1–5.

README gains a `studio` section; `CGO_ENABLED=0 go build ./...` proven; `play`,
`suggest`, `apply` outputs compared byte-for-byte against pre-change captures
(help grows by the one documented `studio` line); one live `studio` session on
`ox-alpha-free` end-to-end (suggest → validate → apply → hear it at the bar
boundary).
