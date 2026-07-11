# CLAUDE.md

## What is watchd?

A Go CLI that allocates finite AI spend across goal-linked agents. Schedules
make agents eligible; verified outcomes, cost, goal weight, uncertainty, and
review debt determine what runs. Without `watchd.yaml`, legacy scheduling is
unchanged.

## Commands

```bash
go build ./cmd/watchd         # build
go test ./...                 # test
./watchd help                 # show all commands
```

## Architecture

Single binary. No runtime dependencies except the `claude` CLI.

```
cmd/watchd/main.go           - entry point
internal/cli/cli.go          - all commands
internal/agent/agent.go      - parse agent markdown files
internal/runner/runner.go    - run claude -p, parse JSON output
internal/store/store.go      - save/query run history as JSON files
internal/daemon/daemon.go    - scheduler loop for recurring agents
internal/portfolio/portfolio.go - policy, goals, statistics, allocation
```

## Agent format

Agents are markdown files in `agents/` with YAML frontmatter:

```yaml
---
name: myagent
schedule: 4h          # interval: 30s, 5m, 2h, 1d
model: sonnet         # claude model
permission_mode: default
max_turns: 10
budget: 0.50          # per-run cost cap in USD, enforced via --max-budget-usd
memory: true          # curated memory/<name>.md injected each run, updated from the run output
gate: true            # read-only dry run proposes a plan; execute only after watchd approve
notify: "ntfy pub ..."  # shell command run on pending/error (WATCHD_* env vars)
goal: product           # goal file under goals/
verify: go test ./...   # optional before/after evidence command
verify_timeout: 2m
---

# Instructions for Claude

The markdown body is the prompt.
```

## Key patterns

- `claude -p` with `--output-format json` returns an array of events on current CLI versions; docs also describe a single-object shape, so `parseOutput` accepts both
- The `result` event has `result`, `total_cost_usd`, `session_id`, `num_turns`, `usage` (token counts), and `is_error`, which can be true even when subtype is "success" (e.g. auth failures), so check both
- Runs stored as JSON files in `runs/`; run ID = filename base, so SaveRun upserts by ID
- Run IDs include nanoseconds so separate fast runs never overwrite each other
- Portfolio mode requires `watchd.yaml`, valid `goals/*.md`, and a positive per-run budget for every scheduled agent
- Allocation is deterministic and model-free: current strategy outcomes, actual average cost, goal weight, and a bounded uncertainty bonus
- Strategy history is scoped by agent hash plus semantic goal hash; changing only goal weight or cap retains history
- `max_unrated` and one-unrated-run-per-agent prevent report production from outrunning review
- Goal authority is a ceiling: observe is read-only, propose is read-only plus pending, act uses configured tools; gate can only restrict further
- Verifiers run before and after action, store at most 8 KiB, and their output is untrusted prompt data
- Outcome ratings are append-only; the allocator uses the latest rating while retaining corrections
- Runs record provenance: `prompt_hash` + `agent_hash` answer "what instructions produced this output"
- `memory: true` makes loops compound: `memory/<name>.md` is injected into the prompt, and the agent returns updated memory after a `===MEMORY===` marker in its final message; watchd extracts it, writes the file, and strips it from the stored result. The agent never writes the file itself: tool-based writes leak into the CLI's own auto-memory directory and don't work on read-only gated passes
- `gate: true` runs a read-only dry run (tool set is the enforcement; plan mode is advisory and ends `-p` runs with an empty result) that must end with a proposed plan; run lands as `pending`, `watchd approve <id>` resumes the session (`--resume session_id`) with the agent's real tools
- `agents/edge-scout.md` is the research bridge for the global Edge Scout skill: manual, memory-backed, budgeted, and approval-gated so watchd owns durable run history while the skill owns the scouting method
- The CLI's `result` field is only the text after the last tool call. A run that ends on a tool call returns an empty result
- `--bare` breaks subscription auth on CLI 2.1.173 ("Not logged in"); revisit before adopting
- Daemon uses simple interval timer, not cron expressions (yet)
- No external dependencies beyond `gopkg.in/yaml.v3`
