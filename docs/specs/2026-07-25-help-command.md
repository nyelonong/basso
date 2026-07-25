# Spec: CLI help command

Lifecycle status: planned

**Roles touched:** none

## Goal

Add a zero-side-effect `basso help` command that prints one concise, complete
reference for every supported Basso command. A user should be able to discover
playback, the bare-file playback alias, AI suggestion, candidate application,
and help itself without opening the README.

## Non-goals

- No interactive help, pager, TUI, shell completion, man page, or generated
  documentation system.
- No command-specific help such as `basso help suggest` in this slice.
- No change to playback, suggestion, candidate, provider, validation, or apply
  behavior.
- No new dependency or CLI framework.
- No renaming of existing commands, flags, or environment variables.

## Constraints

- `basso help` accepts no positional arguments, writes help to stdout, and exits
  successfully.
- `basso -h` and `basso --help` are aliases for the same output because they are
  standard command-line discovery paths.
- Help must not read a pattern or sound directory, construct a provider or
  model, open audio, access the network, or create `.basso/` state.
- The output must list these exact invocation forms:
  - `basso play <source.fnl>`
  - `basso <source.fnl>`
  - `basso suggest [flags] <source.fnl> <prompt>`
  - `basso apply <candidate-id>`
  - `basso help`
- The output must briefly describe `play`, `suggest`, `apply`, and `help`, and
  identify the bare-file form as an alias for `play`.
- The output must list the existing suggestion flags `--provider`, `--model`,
  `--timeout`, and `--sounds`, plus their matching configuration variables:
  `BASSO_AI_PROVIDER`, `BASSO_AI_MODEL`, `BASSO_AI_TIMEOUT`,
  `BASSO_OLLAMA_URL`, and `OPENAI_API_KEY`.
- Existing no-argument, unknown-command, play, suggest, and apply behavior must
  remain unchanged.
- Use the existing injected stdout/stderr and command dependencies so tests
  never touch audio, files, providers, or the network.
- Preserve unrelated worktree changes, including the current `.gitignore` and
  pattern edits.
- Keep the project pure Go and leave `gofmt -l .`, `go vet ./...`,
  `go test -race ./...`, and `CGO_ENABLED=0 go build ./...` green.

## Decision log

| # | Decision | Choice |
|---|---|---|
| 1 | Help entry point | Add `help`; also support conventional `-h` and `--help` aliases |
| 2 | Help depth | One top-level reference; no command-specific help in this slice |
| 3 | Output destination | stdout on success; argument errors continue through the existing stderr error path |
| 4 | Content | Commands, invocation forms, suggestion flags, and provider environment variables |
| 5 | Implementation shape | Reuse the existing standard-library dispatch and injected writers; add no CLI framework |

## Acceptance criteria

### Wave 1: Help behavior

- `basso help`, `basso -h`, and `basso --help` return success and produce
  byte-identical output.
- The output contains every required invocation, command description, flag, and
  environment variable exactly once where uniqueness is meaningful.
- `basso help extra` returns an argument error and does not print successful
  help output.
- Tests prove help never constructs playback, audio, model, or preflight
  dependencies and creates no `.basso/` state.
- Existing dispatch and command tests remain green without weakened assertions.
- Formatting, vet, full race tests, cgo-free build, and diff checks pass.
