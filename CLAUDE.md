# CLAUDE.md

## What is watchd?

A Cloudflare Worker that runs small scheduled AI agents under a hard daily spend
cap. Agents are TypeScript files; results live in D1, show on a dashboard, and
optionally go out by email. The old Go CLI is at the `go-final` tag.

## Commands

```bash
npm test                 # vitest in the Workers runtime
npm run typecheck
npm run dev              # local Worker; secrets in .dev.vars
npm run deploy           # only when explicitly asked
```

## Architecture

```
src/index.ts   - startup validation, scheduled() tick, fetch() -> web
src/run.ts     - reserve budget, fetch sources, call OpenRouter, settle, notify
src/web.ts     - dashboard, token login, public report pages, POST /run/<agent>
src/cron.ts    - five-field UTC cron matcher
src/types.ts   - Env and Agent
agents/        - one file per agent, registered in agents/index.ts
migrations/    - D1 schema (runs, memory)
```

## Key patterns

- One `* * * * *` trigger; each agent's own cron decides if it runs that minute. Cron CPU limit is 30s for sub-hourly triggers, but waiting on the model does not count; wall time is 15 min
- The cap is one `INSERT ... SELECT ... WHERE` statement: today's settled costs plus open reservations plus this budget must fit `DAILY_CAP_USD`. `UNIQUE (agent, slot)` stops a retried tick from running twice
- Cost comes from OpenRouter `usage.cost`; a run that fails without a known cost is charged its full reservation
- Memory: the agent ends its reply with `===MEMORY===` and its updated notes; no marker keeps the old notes
- A reply of exactly `NOTHING` is stored but not emailed or published
- Source text and memory are wrapped in tags and declared data, not instructions
- `RUN_TOKEN` (24+ chars, required) gates everything private: bearer for scripts, a SameSite=Strict cookie holding a hash of the token for the dashboard
- Public pages show only `ok` outputs, never costs or errors
- Email is optional: no `NOTIFY_EMAIL` means no email
- No `secrets.required` block on purpose: it blocks the first deploy until secrets exist and makes local dev drop undeclared `.dev.vars` entries. The Worker fails closed instead (500 without a strong `RUN_TOKEN`, failed run without `OPENROUTER_API_KEY`)
- Keep `compatibility_date` at or below what the installed workerd supports, or tests and `wrangler dev` fail to start
