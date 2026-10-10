# Basso site

The Basso browser player (`/`) and about page (`/about`) for
[Basso](https://github.com/nyelonong/basso). It is a static Vite site
deployed as a Cloudflare Worker with static assets.

## Local development

Requires Node.js `>=22.13.0`.

```sh
npm install
npm run dev
```

The player code lives in `src/player/`. It reads files from the rest of the
repository at build time, so there is one copy of each:

- `sound/808/*.wav`: every sample, bundled as separate files.
- `patterns/*.fnl`: the read-only examples.
- `internal/engine/fennel/compiler.lua`: the Fennel compiler, run in the
  browser on wasmoon (Lua 5.4 in WebAssembly).

The pattern rules mirror the Go engine in `internal/engine/`; change both
together. Audio starts only after the visitor presses **Play**.

## Verification

```sh
npm test
npm run typecheck
npm run build
npx wrangler deploy --dry-run
```

Or run the complete local gate:

```sh
npm run check
```

## Deployment

Cloudflare Workers Builds uses these fields:

- Git repository: `nyelonong/basso`
- Production branch: `master`
- Root directory: `site`
- Build command: `npm run build`
- Deploy command: `npx wrangler deploy`
- Worker name: `basso`
- Custom domain: `basso.afrani.id`

The checked-in `wrangler.jsonc` deploys `dist/` as an assets-only Worker. The
custom domain is a Cloudflare dashboard binding rather than a checked-in route:
open the `basso` Worker, choose **Settings > Domains & Routes > Add > Custom
Domain**, and enter `basso.afrani.id`. Cloudflare creates the DNS record and TLS
certificate when the hostname belongs to the same active Cloudflare account.
