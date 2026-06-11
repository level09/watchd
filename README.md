# watchd

Schedule AI agents. Single binary, zero config.

Agents are markdown files. watchd runs `claude -p` on schedule, tracks runs, monitors cost.

## Install

```bash
go install github.com/level09/watchd/cmd/watchd@latest
```

Or build from source:

```bash
git clone https://github.com/level09/watchd && cd watchd
go build ./cmd/watchd
```

## Quick start

```bash
watchd init              # creates agents/ with an example
watchd run example       # run it once
watchd add myagent       # scaffold a new agent
watchd edit myagent      # write the prompt
watchd up                # start the scheduler
```

## Agent format

Agents are markdown files in `agents/` with YAML frontmatter:

```markdown
---
name: digest
schedule: 1d
model: sonnet
permission_mode: default
max_turns: 10
budget: 0.25
---

# Daily Digest

Summarize the top stories on Hacker News and TechCrunch from the last 24 hours.
Focus on developer tools and AI. Be concise.
```

The frontmatter configures scheduling and cost limits. The body is the prompt Claude follows.

## Commands

| Command | Description |
|---------|-------------|
| `watchd` | Status dashboard |
| `watchd init` | Create agents/ with example |
| `watchd add <name>` | Scaffold a new agent |
| `watchd edit <name>` | Open agent in $EDITOR |
| `watchd run <name>` | Run an agent once |
| `watchd list` | Show all agents |
| `watchd logs [name]` | Run history |
| `watchd costs` | Cost breakdown by agent |
| `watchd pending` | Gated runs awaiting approval |
| `watchd approve <id>` | Execute a pending run's plan |
| `watchd reject <id>` | Discard a pending run |
| `watchd up` | Start scheduler |

## Frontmatter options

| Field | Default | Description |
|-------|---------|-------------|
| `name` | filename | Agent identifier |
| `schedule` | - | Interval: `30s`, `5m`, `2h`, `1d` |
| `model` | `sonnet` | Claude model |
| `permission_mode` | `default` | `default`, `plan`, `full` |
| `max_turns` | - | Limit agentic turns |
| `budget` | - | Max cost per run in USD, enforced mid-run via `--max-budget-usd` |
| `tools` | - | Restrict allowed tools |
| `mcp_config` | - | Path to MCP config JSON |
| `memory` | `false` | Curated `memory/<name>.md` injected each run, updated from the run's output |
| `gate` | `false` | Read-only dry run proposes a plan, execute only after `watchd approve` |
| `notify` | - | Shell command run when a run lands pending or errors |

## Loops that compound

`memory: true` gives each agent a curated memory file: injected into the prompt at the start of every run, and rewritten from the run's output (the agent appends a dated entry and prunes stale ones). A 30-minute scan agent stops re-reporting the same findings and starts building on them, without stuffing raw past output into the prompt.

`gate: true` makes agents safe to point at real systems: the first pass runs with read-only tools and must end with a concrete plan, which lands in `watchd pending`. Approving resumes the same session with the agent's real tools; rejecting discards it.

`notify` closes the async-supervision loop: watchd runs your command (ntfy, Telegram, anything) with `WATCHD_AGENT`, `WATCHD_RUN_ID`, `WATCHD_STATUS`, `WATCHD_RESULT` in the environment, so pending plans reach you instead of waiting to be noticed.

Every run records `prompt_hash` and `agent_hash`, so you can always answer "which instructions produced this output."

## Requirements

- Go 1.21+
- Claude Code CLI (authenticated)

## License

MIT
