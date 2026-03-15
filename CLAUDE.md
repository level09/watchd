# CLAUDE.md

## What is watchd?

A Go CLI that schedules and runs Claude Code agents. Agents are markdown files. watchd runs `claude -p` on schedule, tracks runs, and monitors cost.

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
budget: 0.50          # per-run cost limit in USD
---

# Instructions for Claude

The markdown body is the prompt.
```

## Key patterns

- `claude -p` with `--output-format json` returns a JSON array of events
- Last event with `type: "result"` has `result`, `total_cost_usd`, `session_id`
- Runs stored as JSON files in `runs/` directory
- Daemon uses simple interval timer, not cron expressions (yet)
- No external dependencies beyond `gopkg.in/yaml.v3`
