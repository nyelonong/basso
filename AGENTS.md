# Basso agent guide

Basso is a pure-Go live-coding audio player. It evaluates Fennel patterns and applies edits at the next bar boundary while keeping audio running.

- Read `CLAUDE.md` before changing behavior; it defines the engine boundaries and hot-reload invariants.
- Keep playback local and offline. Only `basso suggest` may call a configured AI provider, and its output is untrusted until local validation succeeds.
- Preserve bar-granular reloads and the open audio device across pattern changes.
- Run `make gates` for normal changes. Use `CGO_ENABLED=0` when validating the pure-Go constraint.
