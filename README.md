# watchd

Schedule Claude Code agents. Single binary, zero config.

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
| `watchd up` | Start scheduler |

## Frontmatter options

| Field | Default | Description |
|-------|---------|-------------|
| `name` | filename | Agent identifier |
| `schedule` | - | Interval: `30s`, `5m`, `2h`, `1d` |
| `model` | `sonnet` | Claude model |
| `permission_mode` | `default` | `default`, `plan`, `full` |
| `max_turns` | - | Limit agentic turns |
| `budget` | - | Max cost per run in USD |
| `tools` | - | Restrict allowed tools |
| `mcp_config` | - | Path to MCP config JSON |

## Requirements

- Go 1.21+
- Claude Code CLI (authenticated)

## License

MIT
