# CLAUDE.md

## What is watchd?

A Cloudflare Worker that runs scheduled AI agents as tool loops (fetch, browser,
search, dedup) under a hard spend cap. Agents live in D1 and are created from the
dashboard or over MCP. The old Go CLI is at the `go-final` tag.

## Commands

```bash
npm test                 # vitest in the Workers runtime
npm run typecheck
npm run dev              # local Worker; secrets in .dev.vars
npm run deploy           # only when explicitly asked
```

## Architecture

```
src/index.ts       - scheduled() -> tick, fetch() -> web
src/run.ts         - enqueue (cap reservation), tick, execute (the tool loop), notify
src/tools.ts       - tool specs and implementations, htmlToText
src/openrouter.ts  - one chat completion call
src/agents.ts      - Agent zod schema and D1 storage
src/web.ts         - dashboard, agent forms, token login, public pages, /run, /runs, /mcp routing
src/mcp.ts         - stateless MCP server (streamable HTTP, JSON responses)
src/markdown.ts    - escaped markdown subset for reports
src/cron.ts        - five-field UTC cron matcher
migrations/        - D1 schema
```

## Key patterns

- Everything runs through the queue: schedules and "Run now" insert a `queued` run; the every-minute tick claims queued runs with a conditional UPDATE and executes them. HTTP requests never run the loop, because `waitUntil` only gets 30s
- The cap is one `INSERT ... SELECT ... WHERE`: today's settled costs plus open reservations plus this budget must fit `DAILY_CAP_USD`. `UNIQUE (agent, slot)` stops a retried tick from running twice
- The tick retires runs stuck in `running` for 20+ minutes (cron wall time is 15) and charges their reservation
- The loop checks the run budget between model calls, so one call can overshoot; at `max_steps` it sends `tool_choice: "none"` and asks for the final answer
- Tool errors go back to the model as text; only provider failures, budget, or a missing final answer fail a run
- `seen` is deterministic dedup by agent and key; model memory (`===MEMORY===`, split on the first marker) is for soft context only
- Tool results and memory are declared untrusted data in the system prompt; reports are escaped before markdown rendering
- Browser Run quick actions need `"remote": true` on the binding even in `wrangler dev`
- `RUN_TOKEN` (24+ chars) gates everything private: bearer for scripts and MCP, a SameSite=Strict cookie holding a hash of the token for the dashboard
- No `secrets.required` block on purpose: it blocks the first deploy until secrets exist and makes local dev drop undeclared `.dev.vars` entries; wrangler reads only `.dev.vars` when it exists, not process env
- Keep `compatibility_date` at or below what the installed workerd supports, and at or above 2026-03-24 for Browser Run quick actions
