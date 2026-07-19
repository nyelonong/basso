# memory-progress

DECISION [direction] what basso becomes → live-coding player (edit pattern code while audio keeps playing; changes apply on the next bar, no restart)
DECISION [hot-reload] how edited patterns reach the running engine → embedded script interpreter (engine re-evaluates the script each bar); patterns are script code, not Go
DECISION [pattern-lang] language for pattern scripts → Fennel (Lisp) running on gopher-lua (pure Go, in-process, no cgo)
DECISION [script-api] how the script describes music → declarative bar function: script exposes pattern(bar) returning hits; engine re-evals and calls it per bar
PLAN live-coding-player written → docs/plans/2026-07-19-live-coding-player.md (8 tasks across 6 DAG waves); spec status set to planned
next: dispatch Wave 1 (1.1 go.mod+deps, 1.2 shell.nix) via /galdr:waves
DECISION [legacy] const.go/m001.go → deleted in wave 1 (early contract, commit 43c39a9), not 6.1; they can't compile vs the go-mix/mix API (OpenAudio/AudioSpec renamed to Start/spec.AudioSpec); pattern preserved in git a7c853e + spec; 6.1 contract reduced to grep proof
DECISION [import-path] audio dep import path → gopkg.in/mix.v0 v0.0.6 (source repo github.com/go-mix/mix declares its module as gopkg.in/mix.v0), NOT the 2020 github.com/go-mix/mix master pin
DECISION [shell.nix] added pkgs.pkg-config + pkgs.sox (commit a41872b) — go-mix/mix/bind transitively imports go-sox (cgo, pkg-config: sox)
EV wave 1 reviewed and closed @8a5a2eb — 1.1 go.mod+go.sum committed (module path, 3 deps, `go build`/`go mod verify`/`gofmt -l .` clean); 1.2 shell.nix already committed @a41872b, `nix-shell --run 'go version'` + SDL2/PortAudio greps pass
DECISION [gate-caveat] `go vet ./...` and `go test ./...` exit 1 with "no packages" right now — the repo has zero .go files after the early const.go/m001.go deletion (commit 43c39a9); this is expected at the end of wave 1 (declarative-only: go.mod + shell.nix, no source yet) and resolves itself once wave 2 task 2.1 adds internal/engine/*.go. Not a gate failure to chase; do not add placeholder .go files to silence it.
next: dispatch Wave 2 (task 2.1 — internal/engine: Hit, PatternProvider, AudioSink, atomixSink, Engine.Run; TDD per brief) via /galdr:waves