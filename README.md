# watchd

Small AI agents that run on a schedule, browse the web, and report back, with a
hard spend cap. One Cloudflare Worker, D1, Browser Run, and OpenRouter.

An agent is a prompt, a schedule, a budget, and a set of tools. When it is due,
watchd runs a tool loop: the model fetches pages, renders JavaScript sites,
searches, and checks which items it has already reported, then writes a markdown
report. Reports show on the dashboard, optionally by email, and on a public page
for agents marked public.

## Tools

| Tool | What it does |
| --- | --- |
| `fetch_page` | Plain HTTP GET, returned as readable text (HTML, feeds, JSON) |
| `browse` | Real browser via Browser Run, page returned as markdown |
| `links` | Real browser, every link on the page |
| `web_search` | Web search through OpenRouter's `:online` models |
| `seen` | Remembers item IDs per agent and returns only new ones |

## Create an agent

From the dashboard: **New agent**, write the instructions, pick tools, set a cron
schedule (UTC) or leave it empty for manual runs.

From Claude Code, over MCP:

```bash
claude mcp add --transport http watchd https://watchd.<you>.workers.dev/mcp \
  --header "Authorization: Bearer $WATCHD_RUN_TOKEN"
```

Then ask Claude to "create a watchd agent that checks X every morning". The MCP
tools are `list_agents`, `get_agent`, `save_agent`, `run_agent`, `get_run` and
`list_runs`.

From a script: `POST /run/<agent>` with `Authorization: Bearer $RUN_TOKEN`
queues a run; `GET /runs/<id>` returns it. Runs start within a minute.

## Spend cap

Each run reserves its budget against `DAILY_CAP_USD` before it is queued, and the
loop stops when the run's spend reaches its budget. Actual cost comes from
OpenRouter per call; a failed run keeps its full reservation. Give watchd its own
OpenRouter key with a credit limit as the outer backstop. Browser Run is billed by
Cloudflare in browser hours (10 per month included on Workers Paid).

## Develop

```bash
npm install
npm test
npm run db:migrate:local
npm run dev          # secrets in .dev.vars; browse and links call Browser Run remotely
curl "localhost:8787/cdn-cgi/local/scheduled?cron=*+*+*+*+*"
```

## Deploy

```bash
npx wrangler d1 create watchd       # put the id in wrangler.jsonc
npm run db:migrate:remote
npm run deploy
npx wrangler secret put OPENROUTER_API_KEY
npx wrangler secret put RUN_TOKEN    # 24+ characters
```

For email, set `NOTIFY_EMAIL` and `EMAIL_FROM` (an address on a domain with
Cloudflare Email Sending) in `wrangler.jsonc`.

The previous Go CLI is preserved at the `go-final` tag.
