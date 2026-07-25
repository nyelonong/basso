# Spec: transactional AI pattern suggestions

Lifecycle status: in-progress

**Roles touched:** none

## Goal

Add an AI-assisted pattern workflow without putting nondeterministic model calls
inside playback. A user asks for a musical change in natural language, Basso sends
only the current Fennel pattern plus a bounded description of the script API and
available sounds to a configured model, and the model returns a candidate Fennel
revision. Basso treats that output as untrusted: it compiles, evaluates, and
validates the candidate before saving it separately from the source. The user sees
the candidate's summary and diff, then explicitly applies it. If a bad manual or
AI-generated edit reaches a running player, the last known-good pattern keeps
playing and Basso reports the rejected revision instead of tearing down audio.

## Non-goals

- No model call from `pattern(bar)`, `Engine.Run`, `AudioSink`, an audio callback,
  or any other playback-time path.
- No autonomous co-performer, unattended prompt loop, or automatic acceptance of
  a model response.
- No generated audio, sample synthesis, audio transcription, microphone input, or
  audio upload to a model.
- No GUI, TUI, editor extension, voice interface, or background daemon.
- No new instrument, effect, pattern combinator, or replacement for the existing
  Fennel `pattern(bar)` contract.
- No session journal, performance replay, offline rendering, stems, or
  collaboration protocol in this release.
- No fine-tuning, embeddings, vector database, retrieval service, or ingestion of
  arbitrary repository files.
- No three-way merge when the source changed after suggestion. A stale candidate
  is refused rather than merged heuristically.
- No provider-side conversation history. Each suggestion is a bounded,
  self-contained request.

## Constraints

- The project remains pure Go with no cgo and keeps the existing
  `github.com/nyelonong/basso` module path.
- Existing `basso play <file.fnl>` and `basso <file.fnl>` behavior remains
  backward compatible.
- Playback remains fully local and network-independent. Basso performs an
  external request only after an explicit `basso suggest` invocation.
- Fennel remains the pattern language and `pattern(bar)` remains the
  script-to-engine contract.
- The audio device is still opened once per play process and remains open across
  accepted and rejected file revisions.
- A pending source is never made active until it successfully evaluates and its
  returned bar passes structural validation.
- After at least one successful bar, a later compile, evaluation, timeout, or
  validation failure is non-fatal: Basso repeats the last successfully produced
  bar, keeps the audio device open, and reports one diagnostic for that failed
  revision and error.
- An invalid initial source remains fatal and must be rejected before the audio
  sink starts; there is no last known-good bar to repeat at startup.
- Every Fennel evaluation runs with a 250 ms context deadline using gopher-lua's
  `LState.SetContext`. Timeout is a validation failure, never an unbounded wait.
- User Fennel executes with only the base, table, string, and math facilities
  needed by the current patterns plus Basso's `bpm` and `steps` host functions.
  Before user source is evaluated, filesystem, process, environment, module
  loading, and debug entry points (`os`, `io`, `debug`, `package`, `require`,
  `dofile`, and `loadfile`) are unavailable.
- A valid bar has BPM in `[20, 400]`, steps per bar in `[1, 256]`, and at most
  4096 hits. Every hit must have a step in `[0, stepsPerBar)`, finite pan in
  `[-1, 1]`, finite velocity in `[0, 1]`, and exactly one of sample or note.
  Note hits must have a valid scientific-pitch note, length in `[1, 4096]`, and
  instrument `bass`, `brass`, or `pluck`. Sample hits must name a basename from
  the configured sound inventory; path separators and traversal are rejected.
- AI candidate preflight evaluates bars 0 through 15 independently through the
  same evaluator and validator used by playback. All 16 bars must pass.
- Suggestion input source and output source are each limited to 256 KiB. The
  user's prompt is non-empty and limited to 16 KiB. Provider response bodies are
  limited to 512 KiB before JSON decoding.
- Model output is untrusted even when a provider reports schema conformance.
  Only local compilation and validation can make a candidate eligible to save.
- A suggestion may make at most two model requests: the initial proposal and one
  repair request containing local diagnostics if the first proposal fails.
- API credentials come only from environment variables, are never written to
  candidate metadata, and are never printed.
- The model receives only the user's prompt, the selected pattern source, the
  fixed Basso script-API description, the allowed sample/instrument inventory,
  and one fixed valid example. It does not receive other repository files,
  environment variables, credentials, git history, or candidate history.
- AI unit and integration tests use fakes or `httptest` servers. Project gates
  never require network access, a model installation, or paid credentials.
- Generated runtime state lives under `.basso/`, which is gitignored. Source
  patterns remain ordinary user-owned `.fnl` files. Basso creates `.basso/` and
  its subdirectories with mode `0700` and candidate, metadata, and backup files
  with mode `0600`.
- All implementation follows TDD and every future wave leaves `gofmt -l .`,
  `go vet ./...`, and `go test ./...` green.

## User workflow

### Configure a provider

The first release supports two explicit provider values:

- `openai` sends a structured-output request to the OpenAI Responses API. It
  requires `OPENAI_API_KEY`.
- `ollama` sends a structured-output request to Ollama's `/api/chat` endpoint.
  `BASSO_OLLAMA_URL` defaults to `http://127.0.0.1:11434`.

Provider and model have no implicit model default because available model names
change independently of Basso. The user supplies them through flags or environment:

```text
--provider              overrides BASSO_AI_PROVIDER
--model                 overrides BASSO_AI_MODEL
--timeout               overrides BASSO_AI_TIMEOUT; default 60s
--sounds                sound inventory directory; default sound/808
```

Missing provider, missing model, an OpenAI invocation without
`OPENAI_API_KEY`, an unsupported provider, an invalid timeout, or a missing
sound inventory fails before reading or sending pattern source. The sounds path
is resolved relative to the invocation directory and contributes only its
regular-file basenames to the model and validator.

### Request a suggestion

```text
basso suggest \
  --provider openai \
  --model <model-name> \
  patterns/indo-bounce.fnl \
  "Keep the kick, make the hats denser, and add a brass response."
```

The final two arguments are exactly one existing regular `.fnl` source path and
one non-empty prompt. The source must be at most 256 KiB and the prompt at most
16 KiB; both limits are checked before a provider request. Suggestion never
opens an audio device and never changes the source file.

The provider must return this logical object under a strict schema with no extra
properties:

```json
{
  "summary": "Short description of the musical change",
  "source": "(bpm 150)\n(steps 16)\n..."
}
```

`summary` is non-empty and at most 500 UTF-8 bytes. `source` is non-empty and at
most 256 KiB. A refusal, truncated response, schema mismatch, oversized field,
HTTP failure, or timeout is an ordinary command error and creates no candidate.

If the first response fails local preflight, Basso makes one repair request that
contains the rejected source and the exact local diagnostics. If the repaired
response also fails, the command exits non-zero, prints both attempts'
diagnostics, and creates no candidate.

### Candidate artifact

A successful suggestion creates two files relative to the invocation directory:

```text
.basso/candidates/<candidate-id>.fnl
.basso/candidates/<candidate-id>.json
```

`candidate-id` is
`<UTC 20060102T150405.000000000Z>-<first 12 lowercase hex characters of candidate SHA-256>`.
Creation uses exclusive file semantics; an existing candidate is never
overwritten.

The JSON metadata has this exact schema-v1 shape:

```json
{
  "schema_version": 1,
  "id": "20260725T120000.123456789Z-0123456789ab",
  "created_at": "2026-07-25T12:00:00.123456789Z",
  "source_path": "/absolute/path/to/pattern.fnl",
  "sounds_path": "/absolute/path/to/sound/808",
  "base_sha256": "<64 lowercase hex characters>",
  "candidate_sha256": "<64 lowercase hex characters>",
  "provider": "openai",
  "model": "<user-selected model>",
  "prompt": "<user prompt>",
  "summary": "<model summary>",
  "attempts": 1,
  "validation": {
    "first_bar": 0,
    "last_bar": 15,
    "timeout_ms_per_bar": 250,
    "status": "passed"
  }
}
```

All fields are required and additional fields are rejected by `apply`.
`created_at` uses RFC 3339 with nanoseconds. `attempts` is 1 or 2, and validation
status in a saved candidate is always `passed`.

It never records credentials, request headers, full provider responses, or
unrelated environment state.

On success, stdout prints the candidate ID, summary, validation result, candidate
path, and a unified diff from the current source. The command exits zero. The
diff is informational; the candidate files are the durable output.

### Apply a candidate

```text
basso apply <candidate-id>
```

`apply` reads the candidate and metadata from the invocation directory's
`.basso/candidates/`. It re-hashes both candidate and current source, reruns the
16-bar local preflight, and refuses if:

- metadata is malformed or has an unsupported schema version;
- either stored hash does not match its file;
- the original source no longer exists;
- the source, candidate, or sounds path is a symbolic link or not the expected
  regular file/directory type;
- the current source hash differs from the recorded base hash;
- validation no longer passes.

Before replacement, `apply` copies the exact current source to:

```text
.basso/backups/<UTC 20060102T150405.000000000Z>-<first 12 base SHA-256 hex>-<source basename>
```

It then writes the candidate to a temporary file in the source directory,
preserves the source file's permission bits, calls `Sync`, closes the temporary
file, and atomically renames it over the source. On success it prints the source
and backup paths. Any failure before rename leaves the source unchanged and
removes the temporary file.

When a separate `basso play` process is watching that source, the directory-based
watcher sees the atomic replacement, stages the new source, and attempts
activation at the next bar boundary. The player performs its own evaluation and
validation again; `apply` preflight never bypasses runtime safety.

## Runtime design

### Last-known-good behavior

`FennelProvider` distinguishes three pieces of state:

- active source: the most recently accepted source revision;
- pending source: the newest debounced filesystem revision not yet accepted;
- last good bar: a defensive copy of the most recent successful
  hits/BPM/steps result.

At each `Next(bar)`:

1. If pending source exists, evaluate and validate it for `bar`.
2. If it succeeds, promote it to active, save a defensive copy of its result as
   last good bar, and return that result.
3. If it fails, discard it, emit a rejection diagnostic, and evaluate active
   source for `bar`.
4. If active succeeds, save a defensive copy as last good bar and return it.
5. If active fails and a last good bar exists, emit a runtime diagnostic and
   return a defensive copy of the last good bar.
6. If no last good bar exists, return the error; startup cannot continue safely.

Runtime diagnostics include revision SHA-256, bar, phase (`compile`,
`evaluate`, `timeout`, or `validate`), and the underlying error. Watcher
diagnostics use phase `watch` and omit bar. The CLI writes diagnostics to stderr.
Identical revision/phase/error diagnostics are emitted once until a different
source revision is observed, preventing one line per bar from flooding the
terminal.

### File watching

The watcher observes the source's parent directory and filters events by cleaned
basename instead of attaching only to the original file inode. This supports
editors and `basso apply` replacing a file by atomic rename. Write, create, and
rename sequences are debounced for 100 ms; the last complete readable source is
staged. Remove without replacement reports a diagnostic while the active source
continues.

### Shared validation

Suggestion preflight, `apply`, initial load, and hot reload use one evaluator and
one structural validator. The CLI must not maintain a weaker duplicate of
runtime rules. Preflight uses a fresh Lua state for every bar so mutation in one
validation bar cannot make another pass accidentally.

## AI boundary

The AI package defines its model-client interface where the suggestion service
consumes it. OpenAI, Ollama, and test fakes return the same provider-neutral
proposal type. Both production adapters use `net/http` with request contexts and
512 KiB response limits; no provider SDK is added. The OpenAI adapter uses the
fixed HTTPS API origin and refuses redirects so its authorization header cannot
be forwarded elsewhere. The Ollama adapter uses only the explicitly resolved
`BASSO_OLLAMA_URL` origin and also refuses redirects.

The prompt template is a versioned embedded asset. It instructs the model to:

- return only the required structured object;
- preserve the user's existing musical material unless the prompt asks to
  remove it;
- use only the supplied script API, samples, and instruments;
- return a complete Fennel file rather than a patch;
- avoid filesystem, network, clock, and external-process assumptions;
- keep `pattern(bar)` bounded and deterministic from its inputs.

Provider schema enforcement reduces transport ambiguity but grants no trust.
The model has no tool capable of applying source, opening audio, reading another
file, or executing a command. Pattern source is delimited as untrusted data in
the prompt; comments inside it cannot change the system instructions or grant
tools.

## Failure behavior

| Failure | Required result |
|---|---|
| Initial pattern invalid | Player exits before audio starts with the diagnostic |
| Watched revision invalid | Revision rejected; active pattern continues |
| Active revision later fails | Last good bar repeats; audio remains open |
| Fennel evaluation exceeds 250 ms | Evaluation cancelled and treated as invalid |
| Provider unavailable or times out | `suggest` fails; no candidate or source change |
| First proposal invalid | One bounded repair request |
| Second proposal invalid | `suggest` fails with both diagnostics; no candidate |
| Candidate stale at apply time | `apply` refuses; no merge and no source change |
| Apply interrupted before rename | Original source remains; temporary file is cleaned |
| Atomic replacement observed live | Runtime independently validates before activation |

## Decision log

| # | Decision | Choice |
|---|---|---|
| 1 | AI's role | Propose complete, auditable Fennel revisions; never produce or schedule audio |
| 2 | First interaction | Separate `basso suggest` command followed by explicit `basso apply` |
| 3 | Playback integration | Existing filesystem reload boundary; no daemon or IPC |
| 4 | Model timing | Model calls occur only in `suggest`, never in playback |
| 5 | Providers | OpenAI cloud and Ollama local adapters in the first release |
| 6 | Provider configuration | Explicit provider and model via flags or environment; no model-name default |
| 7 | Model output | Strict `{summary, source}` object; complete source rather than a patch |
| 8 | Trust boundary | Compile, evaluate, and structurally validate locally regardless of provider schema |
| 9 | Repair loop | At most one diagnostic-guided retry |
| 10 | Validation horizon | Bars 0 through 15, fresh VM per bar, 250 ms per evaluation |
| 11 | Candidate storage | Gitignored `.basso/candidates/` with source plus versioned metadata |
| 12 | Apply safety | Revalidate, require unchanged base hash, create backup, then atomic replace |
| 13 | Bad live edit | Reject pending revision and continue the active source |
| 14 | Later active failure | Repeat the last successfully produced bar and report once |
| 15 | File watching | Watch parent directory so atomic editor and Basso replacements remain observable |
| 16 | Privacy | Send only selected pattern, prompt, fixed API description, inventory, and one example |
| 17 | Dependency policy | Direct bounded HTTP adapters; no provider SDK |
| 18 | Deferred evolution | Autonomous arranging, journals, rendering, UI, and generated audio remain later work |

## Acceptance criteria

### Wave 1 — Transactional runtime safety

- Invalid initial Fennel is rejected before `AudioSink.Start`.
- A valid active source followed by an invalid saved revision keeps scheduling
  the active source, with one start and no teardown during rejection.
- A later valid saved revision activates at the next bar after the invalid one.
- A revision containing an infinite Fennel loop is cancelled within the 250 ms
  evaluation deadline and cannot stop a player with a last good bar.
- A source that succeeds initially but fails on a later bar causes the last good
  bar to repeat without returning a fatal engine error.
- Validation covers every numeric, hit-count, note, instrument, and sample rule
  in Constraints with table-driven tests.
- A real atomic file replacement is observed by the directory watcher and applied
  at a bar boundary.
- Repeated identical failures produce one diagnostic rather than one per bar.
- Existing play aliases and all current patterns remain green.

### Wave 2 — Provider-neutral suggestion core

- A suggestion request contains only the selected source, user prompt, embedded
  API contract/example, and current sound inventory.
- OpenAI and Ollama adapters both enforce the exact proposal schema, use context
  deadlines, bound response size, and map refusal/transport/schema failures to
  provider-neutral errors.
- Provider and model resolution obey flags-over-environment precedence and fails
  before source transmission when configuration is incomplete.
- Fake adapters prove no test requires network or credentials.
- Invalid first output triggers exactly one repair request with local
  diagnostics; valid first output triggers no retry; invalid second output
  creates no candidate.

### Wave 3 — Candidate CLI

- `basso suggest` implements the exact invocation and argument validation in User
  workflow without opening audio or changing the source file.
- Successful proposals pass all 16 preflight bars and create exclusive `.fnl`
  and schema-v1 `.json` candidate files with correct hashes and provenance.
- Candidate ID, summary, validation result, path, and unified diff are printed.
- Refusal, timeout, oversized output, malformed schema, or failed preflight exits
  non-zero and creates no candidate.
- `.basso/` is ignored without broadening ignores for user pattern files.

### Wave 4 — Safe apply and live handoff

- `basso apply` verifies metadata, hashes, unchanged base, and fresh preflight
  before touching the source.
- A stale candidate refuses without changing the source or creating a misleading
  successful backup.
- A valid candidate creates an exact backup, preserves source permissions, and
  atomically replaces the source.
- An injected failure at every pre-rename filesystem step leaves the original
  source byte-for-byte unchanged.
- End-to-end tests with a fake provider and fake audio sink prove:
  suggest creates a candidate, apply replaces the source, a running provider
  observes it, and the validated revision becomes active at the next bar with no
  audio restart.
- README and CLI usage document cloud consent, local Ollama use, candidate
  review, apply conflict refusal, backups, and the fact that model output remains
  locally validated.
- `gofmt -l .`, `go vet ./...`, and `go test ./...` pass with no network access.
