# watchd

Small AI agents on a schedule, with a hard spend cap. One Cloudflare Worker, D1,
and OpenRouter.

Each agent is a TypeScript file: a cron schedule, a budget, a prompt, and
optional source URLs. When it is due, watchd fetches the sources, makes one model
call, stores the result, and optionally emails it to you. A dashboard shows spend,
runs, and a "Run now" button. Agents marked `public` get a shareable report page.

## Add an agent

```ts
// agents/release-radar.ts
import type { Agent } from "../src/types";

export default {
  name: "release-radar",
  schedule: "0 7 * * 1", // cron, UTC
  budget: 0.05, // USD reserved per run
  memory: true, // keep notes between runs
  public: false, // true publishes reports at /p/release-radar
  sources: ["https://github.com/honojs/hono/releases.atom"],
  prompt: "List releases worth acting on, with links.",
} satisfies Agent;
```

Register it in `agents/index.ts` and deploy. A reply of exactly `NOTHING` stores
the run without emailing. Append `:online` to a `model` slug for OpenRouter web
search.

## Spend cap

Before each call watchd reserves the agent's budget and refuses the run if the
UTC day would exceed `DAILY_CAP_USD`. After the call it records OpenRouter's
reported cost; a failed run keeps its full reservation. Give watchd its own
OpenRouter key with a credit limit as the outer backstop.

## Develop

```bash
npm install
npm test
npm run db:migrate:local
npm run dev          # http://localhost:8787, secrets in .dev.vars
curl "localhost:8787/cdn-cgi/local/scheduled?cron=*+*+*+*+*"
```

## Deploy

```bash
npx wrangler d1 create watchd       # put the id in wrangler.jsonc
npm run db:migrate:remote
npx wrangler secret put OPENROUTER_API_KEY
npx wrangler secret put RUN_TOKEN    # openssl rand -hex 24
npm run deploy
```

Sign in to the dashboard with `RUN_TOKEN`, or run an agent from a script:

```bash
curl -X POST -H "authorization: Bearer $RUN_TOKEN" https://watchd.<you>.workers.dev/run/edge-scout
```

For email, set `NOTIFY_EMAIL` and `EMAIL_FROM` (an address on a domain with
Cloudflare Email Sending) in `wrangler.jsonc`.

The previous Go CLI is preserved at the `go-final` tag.
