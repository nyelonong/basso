# galdr config — basso

## Gates

Task-level and gate-level commands. Run inside `nix-shell` (or `make gates`).

- `gofmt -l .`
- `go vet ./...`
- `go test ./...`

No blockers — `go.mod` exists, pure-Go, `CGO_ENABLED=0` clean, all three
commands pass on a clean checkout.

## Invariants

(none yet)

## Fast path

(none — default route criteria apply)

## Review sources

(none — README is a stub; add a style/design doc here when one exists)

## Models

- mechanical: claude-haiku-4-5
- standard: claude-sonnet-5
- top: session model

## Worktree notes

- Language: Go, pure-Go dependency tree, `CGO_ENABLED=0` clean — a fresh
  worktree just needs `nix-shell` (provides Go) or a system Go toolchain, no
  other setup.
- No env files to copy.
- Service dependencies: none. `github.com/gopxl/beep/v2`'s audio backend
  (`ebitengine/oto`/`purego`) talks to the OS audio API directly via
  dlopen-based FFI — no native libs, no nix packages beyond Go itself. No
  background services / docker compose.

## Smoke

- Launch: `nix-shell --run 'go run ./cmd/basso play patterns/basic-groove.fnl'`
  (or `make run`)
- Base URL: none — output is audio played to the sound device, plus a printed
  `bar <n> bpm <bpm>` line per bar.
- Test account / seed data: none.
- Smoke-sheet output dir: `docs/smoke/`
- Requires: a working audio device to confirm real playback (the process
  itself runs and exits cleanly even without one — only actual audible sound
  needs a real device, and that specifically needs a human listening, an
  agent cannot verify it directly).

## Briefs

Waves' dispatch briefs under `docs/briefs/` are **gitignored** by default (the
galdr default). Commit them only if you explicitly override this.

## Budget

- `rate-limits-cache`: `~/.claude/rate-limits-cache.json`
- `five-hour-park-pct`: `90`
- `seven-day-park-pct`: `95`
- `rate-limits-max-age`: `300`