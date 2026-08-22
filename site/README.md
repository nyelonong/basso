# Basso site

The public product page for [Basso](https://github.com/nyelonong/basso).
It is a static Vite site deployed as a Cloudflare Worker with static assets.

## Local development

Requires Node.js `>=22.13.0`.

```sh
npm install
npm run dev
```

The sequencer uses four of Basso's bundled 808 samples. Audio starts only after
the visitor presses **Play bar**.

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
