# basso

A live-coding player: a persistent process that plays a Fennel pattern
continuously and reloads it at the next bar boundary when you save the
source file, with no audio restart.

[Website](https://basso.afrani.id) · [Source](https://github.com/nyelonong/basso)

### Install

    make install

Installs to `$(go env GOPATH)/bin/basso`. No other dependencies — pure Go,
no cgo, no native libraries.

### Play

    basso play patterns/basic-groove.fnl

Or without installing:

    make run FILE=patterns/basic-groove.fnl

Edit the `.fnl` file and save while it's playing — the change takes effect
at the next bar. Ctrl-C to stop.

### Studio cockpit

    basso studio
    basso studio patterns/basic-groove.fnl

Without a file, studio opens a flat picker for `.fnl` files in the launch
directory. Press `n` to name a silent blank pattern. Press `N` to describe a
new pattern once; studio derives a unique filename and submits that description
to the AI automatically. During playback, `space` pauses/resumes at a bar
boundary, `x` stops and resets to bar 0, and `p` plays again.

Press `s` to describe an edit. After local validation, studio automatically
auditions the candidate at the next bar while leaving the real file untouched.
Press `a` to promote it through the transactional backup path or `r` to return
to the real file and delete it. `esc` cancels an in-flight request; `q` discards
an armed candidate, stops playback, and quits. Provider setup is only needed
when you use an AI prompt.

### Develop

    make build   # bin/basso
    make test
    make vet
    make fmt     # gofmt -l .
    make gates   # fmt + vet + test

### AI suggestions

Playback stays entirely local and offline. Basso makes a provider request only
when you explicitly run `basso suggest` or submit a studio prompt; `play` and
hot reload never need network access.

Before running a suggestion, choose and configure a provider and model. No
model is selected by default:

```sh
# OpenAI
export BASSO_AI_PROVIDER=openai
export BASSO_AI_MODEL="your-model-name"
export OPENAI_API_KEY="your-api-key"

# Or Ollama (its URL is optional and defaults to http://127.0.0.1:11434)
export BASSO_AI_PROVIDER=ollama
export BASSO_AI_MODEL="your-model-name"
export BASSO_OLLAMA_URL=http://127.0.0.1:11434
```

Any OpenAI-compatible gateway works too — point `BASSO_AI_BASE_URL` at its
base URL and set `BASSO_AI_API_KEY`. Instead of exports, these variables can
come from a `.env` file in the directory you run basso from (real environment
variables win); keep that file out of version control and readable only by
you (`chmod 600 .env`).

Your explicit consent to `basso suggest` sends only the selected source, your
prompt, Basso's fixed API/example, and the allowed sample and instrument names
to the selected provider. It does not send other repository files, environment
variables, credentials, git history, or candidate history.

Flags override environment configuration: `--provider`, `--model`,
`--timeout`, and `--sounds`.

```text
basso suggest [flags] <source.fnl> "<prompt>"
```

Treat model output as untrusted. Basso runs local validation across 16 bars
before it saves a candidate, again before it applies one, and again through live
activation. Review stdout before applying: it reports the candidate ID, summary,
validation result, candidate path, and a unified diff. Successful candidates
are durable local files:

```text
.basso/candidates/<candidate-id>.fnl
.basso/candidates/<candidate-id>.json
```

The `.basso/` runtime state is gitignored; your `.fnl` patterns are not.

Apply only the reviewed candidate:

```sh
basso apply <candidate-id>
```

Apply preserves the original source under `.basso/backups/` and reports the
exact backup path. To recover manually, set `BACKUP_PATH` to that exact printed
path, then copy it over the source:

```sh
cp "$BACKUP_PATH" patterns/basic-groove.fnl
```

If you edit the source manually after requesting a candidate, its base is
stale and `basso apply` refuses it rather than overwriting your change. A failed
live revision also keeps the last known-good music playing.
