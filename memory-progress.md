# memory-progress

DECISION [direction] what basso becomes → live-coding player (edit pattern code while audio keeps playing; changes apply on the next bar, no restart)
DECISION [hot-reload] how edited patterns reach the running engine → embedded script interpreter (engine re-evaluates the script each bar); patterns are script code, not Go
DECISION [pattern-lang] language for pattern scripts → Fennel (Lisp) running on gopher-lua (pure Go, in-process, no cgo)
DECISION [script-api] how the script describes music → declarative bar function: script exposes pattern(bar) returning hits; engine re-evals and calls it per bar
PLAN live-coding-player written → docs/plans/2026-07-19-live-coding-player.md (8 tasks across 6 DAG waves); spec status set to planned
next: dispatch Wave 1 (1.1 go.mod+deps, 1.2 shell.nix) via /galdr:waves
**WIP** dispatched docs/briefs/1.1-brief.md @6856b91 scope=go.mod,go.sum — next: review on return
**WIP** dispatched docs/briefs/1.2-brief.md @6856b91 scope=shell.nix — next: review on return
DECISION [legacy] const.go/m001.go → deleted in wave 1 (early contract, commit 43c39a9), not 6.1; they can't compile vs the go-mix/mix API (OpenAudio/AudioSpec renamed to Start/spec.AudioSpec); pattern preserved in git a7c853e + spec; 6.1 contract reduced to grep proof
DECISION [import-path] audio dep import path → gopkg.in/mix.v0 v0.0.6 (source repo github.com/go-mix/mix declares its module as gopkg.in/mix.v0), NOT the 2020 github.com/go-mix/mix master pin
DECISION [shell.nix] added pkgs.pkg-config + pkgs.sox (commit a41872b) — go-mix/mix/bind transitively imports go-sox (cgo, pkg-config: sox)