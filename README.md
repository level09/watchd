# watchd

Wake up to what changed.

Your agents are markdown files. watchd runs them on schedule with `claude -p`, tracks every dollar, remembers what each run found, and gates anything dangerous behind your approval. One Go binary. Zero config.

```bash
go install github.com/level09/watchd/cmd/watchd@latest
```

## One night

```
23:08 » watchd up
        5 agents armed. waiting for schedules.

00:30 ✓ uptime      $0.008   all healthy, 200 OK in 142ms
01:00 ✓ errors      $0.021   3 new timeouts in payment worker, same root cause
02:00 ✓ security    $0.214   raw SQL in new search endpoint, flagged with patch
04:15 ✓ competitor  $0.041   Acme cut Pro pricing 20%, second cut this quarter
06:00 ✓ digest      $0.089   morning brief compiled, 1 plan pending your approval
```

You slept. They didn't. Total: $0.37.

## Sixty seconds to your first agent

```bash
watchd init          # creates agents/example.md
watchd run example
```

An agent is one file. Frontmatter is the config, the body is the prompt. Save this as `agents/uptime.md`:

```markdown
---
name: uptime
schedule: 5m
model: haiku
budget: 0.02
---

Check if https://api.myapp.com/health returns 200. If the response is
slow or the body looks wrong, explain what might be happening.
```

```
$ watchd run uptime
✓ uptime in 4.2s ($0.0089)
The endpoint returned HTTP 200 in 142ms. All healthy.

$ watchd up          # start the scheduler
```

That is the whole loop. Everything below is what makes it compound.

## Memory: loops that compound

A scanner that re-reports the same findings is noise. Add one line of frontmatter and the agent gets a notes file it curates itself, injected at the start of every run and rewritten at the end:

```markdown
---
name: competitor
schedule: 6h
model: haiku
memory: true
budget: 0.10
---

Check the pricing pages of Acme, Initech and Globex. Build on what you
already know. Report only what changed and what it means.
```

Run 1 writes a baseline. Run 2 reports only the delta. Run 3 connects the dots:

```
$ cat memory/competitor.md

## Baseline
- Acme: Pro $49 -> $39 (-20%)
- Initech: usage-based, no free tier
- Globex: enterprise only, POC required

## 2026-06-09
- Acme launches annual billing

## 2026-06-11
- second Acme price cut this quarter
- pattern: price war forming, watch for the Globex response
```

This is curated memory, not transcript stuffing. The model rewrites its own notes each run, so context stays sharp, stale entries get pruned, and a poisoned page scraped in run 12 never becomes standing instructions for run 13.

## The gate: safe to point at real systems

Some agents should not act on their own. With `gate: true` the run gets read-only tools and must end with a concrete plan. Nothing executes until you approve.

```markdown
---
name: dbcleanup
schedule: 1d
model: sonnet
gate: true
notify: "ntfy pub alerts 'watchd: $WATCHD_AGENT $WATCHD_STATUS'"
---

Find bloated tables, unused indexes, and rows older than the retention
policy. Propose a cleanup plan with exact commands and expected impact.
```

```
$ watchd pending
RUN                          AGENT      PROPOSED
dbcleanup_2026-06-12_060003  dbcleanup  1. VACUUM ANALYZE on 4 bloated tables
                                        2. Drop unused index idx_sessions_legacy
                                        3. Archive 48,210 rows >180d from events

$ watchd approve dbcleanup_2026-06-12_060003
```

Approving resumes the same session with the agent's real tools, so it executes exactly the plan you read. `watchd reject` discards it. The `notify` command fires the moment something lands pending or fails, with `WATCHD_AGENT`, `WATCHD_RUN_ID`, `WATCHD_STATUS` and `WATCHD_RESULT` in the environment, so the plan reaches your phone instead of waiting to be noticed.

## Why not cron + bash

Cron runs scripts. watchd runs judgment.

A bash script checks if the endpoint returned 200. An agent notices the response was 200 but took four seconds, that the body is an error page wearing a success code, that the same timeout pattern showed up last Tuesday. You describe the intent in plain language; the model does the interpreting.

On top of that, cron gives you none of the operational layer: no cost tracking, no run history once the terminal closes, no memory between runs, and your script executes with your full permissions from minute one. watchd tracks cost per run and enforces budgets mid-run, keeps every run as a queryable record, compounds findings through memory, and holds dangerous work behind the gate.

## Commands

| Command | What it does |
|---------|--------------|
| `watchd` | Status dashboard: last run, cost, schedule per agent |
| `watchd init` | Create `agents/` with an example |
| `watchd add <name>` | Scaffold a new agent |
| `watchd edit <name>` | Open agent in `$EDITOR` |
| `watchd run <name>` | Run an agent once |
| `watchd up` | Start the scheduler |
| `watchd logs [name]` | Run history |
| `watchd costs` | Spend per agent |
| `watchd pending` | Gated runs awaiting approval |
| `watchd approve <id>` | Execute a pending plan |
| `watchd reject <id>` | Discard a pending plan |

## Frontmatter

| Field | Default | Description |
|-------|---------|-------------|
| `name` | filename | Agent identifier |
| `schedule` | none | Interval: `30s`, `5m`, `2h`, `1d` (empty = manual only) |
| `model` | `sonnet` | Claude model (`haiku` for cheap high-frequency loops) |
| `budget` | none | Max cost per run in USD, enforced mid-run |
| `memory` | `false` | Curated memory file, injected and rewritten every run |
| `gate` | `false` | Read-only dry run, execute only after approval |
| `notify` | none | Shell command fired on pending or error |
| `max_turns` | none | Limit agentic turns |
| `permission_mode` | `default` | Claude permission mode |
| `tools` | minimal set | Restrict allowed tools |
| `mcp_config` | none | Path to MCP config JSON (none loaded by default) |

## Under the hood

watchd is a thin orchestration layer, about 1,000 lines of Go. No AI runtime, no API keys to manage. It spawns `claude -p`, parses the JSON output, and records every run with its cost, token counts, and a hash of the exact instructions that produced it, so you can always answer "which prompt did this."

```
cmd/watchd          entry point, one binary, no runtime deps
internal/cli        all commands
internal/agent      markdown + YAML frontmatter parsing
internal/runner     spawns claude -p, memory, gate, notify
internal/store      run history as JSON files with provenance
internal/daemon     scheduler loop
```

Requires Go 1.21+ and an authenticated [Claude Code](https://claude.com/claude-code) CLI.

## License

MIT
