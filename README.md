# watchd

Schedule, run, and track AI agents with zero infra.

One SQLite file. No Redis, no Docker, no queue. Just `pip install watchd` and a Python file.

## Install

```bash
pip install watchd
```

## Quick start

```python
# app.py
from watchd import Watchd, every

app = Watchd()

@app.agent(schedule=every.minutes(5))
def site_check(ctx):
    import httpx
    resp = httpx.get("https://example.com")
    ctx.state["last_status"] = resp.status_code
    return resp.status_code

@app.agent(schedule=every.day.at("09:00"))
def daily_report(ctx):
    history = ctx.history  # last 10 runs across all agents
    ctx.log.info("generating_report", runs=len(history))
    return "report done"
```

Run it:

```bash
watchd start              # start scheduler
watchd run site_check     # run one agent now
watchd list               # list agents + schedules
watchd history            # show run history
watchd state site_check   # show persisted state
```

## Scheduling

```python
from watchd import every

every.hour                       # every hour
every.minutes(30)                # every 30 minutes
every.seconds(10)                # every 10 seconds
every.day.at("03:00")            # daily at 3 AM
every.monday.at("09:00")         # weekly on Monday at 9 AM
every.cron("*/5 * * * *")        # raw crontab
```

## Agent context

Every agent receives a `ctx` object with:

- **`ctx.state`** - persistent key/value store (dict-like, backed by SQLite)
- **`ctx.log`** - structured logger (structlog)
- **`ctx.history`** - last 10 runs for this agent
- **`ctx.agent_name`** - the agent's name
- **`ctx.run_id`** - unique ID for the current run

## LLM integration

watchd doesn't wrap LLM clients. Use whatever SDK you want:

```python
@app.agent(schedule=every.hour)
def reviewer(ctx):
    from anthropic import Anthropic
    client = Anthropic()
    resp = client.messages.create(
        model="claude-sonnet-4-20250514",
        max_tokens=1024,
        messages=[{"role": "user", "content": "Review recent logs"}]
    )
    ctx.state["last_review"] = resp.content[0].text
    return resp.content[0].text
```

## Retries

```python
@app.agent(schedule=every.minutes(5), retries=3)
def flaky_check(ctx):
    # will retry up to 3 times on exception
    ...
```

## How it works

watchd is three things:

1. **Scheduler** - APScheduler 3.x wrapping your decorated functions
2. **Run tracker** - every execution is logged with status, timing, result, and errors
3. **State store** - per-agent key/value persistence across runs

Everything lives in a single SQLite file (`watchd.db` by default).

## License

MIT
