<div align="center">
  <h1>watchd</h1>
  <h3>Watch anything. Diff it. Act on the change. One SQLite file.</h3>
</div>

<div align="center">
  <a href="https://pypi.org/project/watchd/"><img src="https://img.shields.io/pypi/v/watchd.svg" alt="PyPI"></a>
  <a href="https://pypi.org/project/watchd/"><img src="https://img.shields.io/pypi/pyversions/watchd.svg" alt="Python"></a>
  <a href="https://github.com/level09/watchd/blob/master/LICENSE"><img src="https://img.shields.io/github/license/level09/watchd.svg" alt="License"></a>
</div>

---

Point watchd at a URL, file, or command. It fetches on schedule, diffs against the last run, and optionally asks an LLM whether the change matters. State, history, and notifications in one SQLite file. No Docker, no Redis, no queue.

## Install

```bash
uv add "watchd[ai]"
```

Or `pip install "watchd[ai]"` for both Anthropic and OpenAI support. Use `watchd[anthropic]` or `watchd[openai]` for just one.

## Quick start

### Zero-code: watch anything from the terminal

```bash
watchd watch https://competitor.com/pricing --every 5m
watchd watch /etc/nginx/nginx.conf --every 1h
watchd watch "df -h /" --cmd --every 10m
```

### Full agent: drop a Python file

```bash
watchd init          # creates watchd.toml + watchd_agents/
watchd new my_agent  # scaffold a new agent
watchd run my_agent  # run once, see what happens
watchd up            # start all agents on their schedules
```

An agent is a function with a schedule:

```python
from watchd import agent, watch

@agent(every="5m")
def pricing_watch(ctx):
    change = watch.url("https://acme.com/pricing", ctx=ctx)
    if change is None:
        return "no change"

    ctx.notify(f"Pricing changed: {change.summary}", channel="slack")
    return change.summary
```

`watch.url()` fetches the page and diffs against the previous run. Returns a `Change` object or `None`. No state plumbing required.

## Watch primitives

Three ways to watch things. All return `Change` or `None`:

```python
# Watch a URL (auto-fetches and diffs)
change = watch.url("https://api.vendor.com/status", ctx=ctx, headers={"Authorization": "..."})

# Watch a file (full diff or tail mode for append-only logs)
change = watch.file("/var/log/app.log", ctx=ctx, mode="tail")

# Watch a command's output
change = watch.command("df -h / | tail -1", ctx=ctx)
```

The `Change` object:

| Field | Description |
|-------|-------------|
| `change.before` | Previous content |
| `change.after` | Current content |
| `change.diff` | Unified diff |
| `change.summary` | e.g. "5 added, 2 removed" |
| `change.new` | New lines only (tail mode) |

## LLM judge

Send any diff to an LLM for a yes/no verdict:

```python
@agent(every="1d")
def tos_watch(ctx):
    change = watch.url("https://vendor.com/terms", ctx=ctx)
    if change:
        verdict = ctx.judge(
            change,
            instruction="Did anything change about pricing, liability, or data retention?"
        )
        if verdict.should_act:
            ctx.notify(f"ToS alert: {verdict.summary}", channel="slack")
```

`ctx.judge()` returns a `Verdict` with `should_act` (bool), `summary` (str), and `raw` (full LLM response). Requires `watchd[anthropic]` or `watchd[openai]`.

## Notifications

```python
ctx.notify("message")                                          # stdout
ctx.notify("message", channel="slack", url="https://hooks.slack.com/...")
ctx.notify("message", channel="webhook", url="https://myteam.com/hooks/alerts")
```

## Examples

### SSL certificate expiry

```python
@agent(every="12h")
def ssl_watch(ctx):
    change = watch.command(
        "echo | openssl s_client -connect mysite.com:443 2>/dev/null"
        " | openssl x509 -noout -enddate",
        ctx=ctx,
    )
    if change:
        ctx.notify(f"SSL cert changed: {change.summary}", channel="slack")
```

### Disk usage spikes

```python
@agent(every="5m")
def disk_watch(ctx):
    change = watch.command("df -h / | tail -1", ctx=ctx)
    if change:
        verdict = ctx.judge(change, instruction="Is disk usage above 85%?")
        if verdict.should_act:
            ctx.notify(verdict.summary, channel="slack")
```

### Log file tailing

```python
@agent(every="30s")
def error_watch(ctx):
    change = watch.file("/var/log/app.log", ctx=ctx, mode="tail")
    if change and "ERROR" in change.new:
        ctx.notify(f"{change.summary}: {change.new[:200]}", channel="slack")
```

### API schema drift

```python
@agent(every="1h")
def api_watch(ctx):
    change = watch.url("https://api.vendor.com/v2/status", ctx=ctx)
    if change:
        verdict = ctx.judge(change, instruction="Did the API schema change?")
        if verdict.should_act:
            ctx.notify(f"API drift: {verdict.summary}", channel="webhook",
                       url="https://myteam.com/hooks/alerts")
```

### Competitor pricing

```python
@agent(every="6h")
def pricing_watch(ctx):
    change = watch.url("https://competitor.com/pricing", ctx=ctx)
    if change:
        verdict = ctx.judge(change, instruction="Did any price change?")
        if verdict.should_act:
            ctx.notify(verdict.summary, channel="slack")
```

## Agent context

Every agent gets a `ctx`:

| | |
|---|---|
| `ctx.state` | Persistent dict, SQLite-backed. Survives restarts. |
| `ctx.log` | Structured logger (structlog) |
| `ctx.history` | Last 10 runs for this agent |
| `ctx.notify()` | Push to stdout, Slack, or webhook |
| `ctx.judge()` | LLM verdict on a Change |
| `ctx.agent_name` | Agent name |
| `ctx.run_id` | Current run ID |

## Scheduling

String shorthand in the decorator:

```python
@agent(every="30s")     # every 30 seconds
@agent(every="5m")      # every 5 minutes
@agent(every="2h")      # every 2 hours
@agent(every="1d")      # every day
@agent(every="0 9 * * 1-5")  # cron: weekdays at 9am
```

Or the DSL:

```python
from watchd import every

@agent(schedule=every.minutes(5))
@agent(schedule=every.hour)
@agent(schedule=every.day.at("09:00"))
@agent(schedule=every.monday.at("08:00"))
@agent(schedule=every.cron("*/5 * * * *"))
```

## CLI

```
watchd init                    scaffold project
watchd new <name>              create agent file
watchd list [--as-json]        show agents + schedules
watchd status [agent] [--as-json]  last run per agent
watchd run <name> [--as-json]  run one now
watchd up                      start scheduler
watchd watch <target>          zero-code watcher
watchd history [agent] [--as-json]  run history
watchd logs [agent] [--run-id]  captured output
watchd state <name> [--as-json]  inspect persisted state
watchd deploy                  git pull + restart on server
watchd install                 install systemd user service
watchd uninstall               remove systemd service
```

## Agent directory layout

```
watchd_agents/
  simple.py              # discovered directly
  analyzer/              # directory-based agent
    agent.py             # entry point
    system.txt           # agent reads via Path(__file__).parent
    _helpers.py          # skipped (underscore prefix)
  _internal/             # skipped (underscore prefix)
```

## How it works

1. APScheduler fires your agents on schedule
2. `watch.*()` fetches content, diffs against state stored in SQLite
3. Each run is logged with status, timing, stdout, and errors
4. State is flushed to SQLite after each execution

One file, one process.

## Development

```bash
git clone https://github.com/level09/watchd.git
cd watchd
uv sync --extra ai
uv run pytest
uv run ruff check src/ tests/
```

## License

MIT
