# galdr config — basso

## Gates

Task-level and gate-level commands.

- `gofmt -l .`
- `go vet ./...`
- `go test ./...`

> **Blocker:** no `go.mod` exists. The three commands above require a Go module
> to run. Add `go mod init` (e.g. `go mod init github.com/nyelonong/basso`) and
> `go get github.com/go-mix/mix` before these gates will pass.
>
> **Skip note (audio):** end-to-end runs (`go run const.go m001.go`) require the
> system libraries SDL2 and PortAudio and a working audio device. In any
> environment lacking them, the run skips — a skipped run is not a clean pass.

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

- Language: Go. No `go.mod` yet (see Gates) — a fresh worktree needs the same
  `go mod init` + `go get` before it builds.
- No env files to copy.
- Service dependencies: system audio libraries — SDL2 and PortAudio. Install
  via **nix** (e.g. `nix-shell -p SDL2 portaudio --run 'go run const.go m001.go'`,
  or a `shell.nix` / flake devshell). Do not use brew. The README's apt commands
  target Debian/Ubuntu. No background services / docker compose.

## Smoke

- Launch: `go run const.go m001.go`
- Base URL: none — output is audio played to the sound device, plus one printed
  status line (`Atomix, pid:..., spec:...`).
- Test account / seed data: none.
- Smoke-sheet output dir: `docs/smoke/`
- Requires: SDL2 + PortAudio installed and a working audio device. Without them,
  smoke is a skip, not a pass.

## Briefs

Waves' dispatch briefs under `docs/briefs/` are **gitignored** by default (the
galdr default). Commit them only if you explicitly override this.

## Budget

- `rate-limits-cache`: `~/.claude/rate-limits-cache.json`
- `five-hour-park-pct`: `90`
- `seven-day-park-pct`: `95`
- `rate-limits-max-age`: `300`