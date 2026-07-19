# memory-progress

DECISION [direction] what basso becomes → live-coding player (edit pattern code while audio keeps playing; changes apply on the next bar, no restart)
DECISION [hot-reload] how edited patterns reach the running engine → embedded script interpreter (engine re-evaluates the script each bar); patterns are script code, not Go
DECISION [pattern-lang] language for pattern scripts → Fennel (Lisp) running on gopher-lua (pure Go, in-process, no cgo)
DECISION [script-api] how the script describes music → declarative bar function: script exposes pattern(bar) returning hits; engine re-evals and calls it per bar
PLAN live-coding-player written → docs/plans/2026-07-19-live-coding-player.md (8 tasks across 6 DAG waves); spec status set to planned
next: dispatch Wave 1 (1.1 go.mod+deps, 1.2 shell.nix) via /galdr:waves