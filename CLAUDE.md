# CLAUDE.md

This file provides guidance to Claude Code (claude.ai/code) when working with code in this repository.

## What is watchd?

Scheduling + Agno agent execution + run history in one library. One SQLite file holds everything. No Docker, no Redis.

v2 is built on Agno for the agent runtime, giving agents persistent memory, tool integrations (HN, Notion, Slack, GitHub), structured outputs, and compounding intelligence. watchd provides scheduling, CLI, discovery, and zero-infra deployment.

## Commands

```bash
uv run python -m pytest              # run all tests
uv run python -m pytest tests/test_store.py  # run a single test file
uv run python -m pytest -k test_name # run a single test
uv run ruff check src/ tests/        # lint
uv run ruff format src/ tests/       # format
uv sync                              # install deps
uv sync --extra anthropic            # install with anthropic provider
```

CLI entry point: `watchd` (mapped to `watchd.cli:main`).

## Architecture

Modules in `src/watchd/`, each with one job:

- **app.py** (`Watchd`) - Orchestrator. Holds agent registry, wires APScheduler to runner, manages lifecycle. Lazy-creates shared Agno `SqliteDb` for memory persistence.
- **registry.py** - `@agent()` decorator with Agno params (model, tools, instructions, learning, output_schema). `AgentEntry` dataclass. Global `_registry` dict.
- **runner.py** - `execute_agent()`: two modes. Simple mode (no-arg fn returns prompt, watchd builds Agno Agent). Full mode (fn takes ctx, builds its own Agent). Captures stdout, records runs.
- **discovery.py** - `discover_agents(agents_dir)` scans `*.py` files and `*/agent.py` subdirectories, imports via `importlib.util`, returns registered agents.
- **schedule.py** - `every` singleton with fluent DSL. Produces frozen `Schedule` dataclasses. APScheduler import deferred to `to_apscheduler_trigger()`.
- **store.py** - Raw `sqlite3` + dataclasses. Two tables: `agents`, `runs`. WAL mode, foreign keys on.
- **config.py** - `Config` dataclass with defaults including `model` and `learning`. `load_config()` reads `watchd.toml`.
- **cli.py** - cyclopts commands: `init`, `new`, `up`, `run`, `list`, `history`, `logs`, `status`, `memory`, `doctor`, `deploy`.
- **output.py** - Rich console helpers.
- **deploy.py** - systemd service management.

### Agent authoring API

Simple mode (function returns prompt, watchd builds Agno Agent):
```python
@agent(
    every="4h",
    model="anthropic:claude-sonnet-4-5-20250929",
    tools=[HackerNewsTools()],
    learning=True,
    instructions=["Find AI leverage ideas on Show HN."],
)
def showhn():
    return "Analyze today's Show HN posts."
```

Full mode (function takes ctx, builds its own Agent):
```python
@agent(every="6h")
def analyzer(ctx):
    from agno.agent import Agent
    from agno.models.anthropic import Claude
    agent = Agent(model=Claude(id="claude-sonnet-4-5-20250929"), db=ctx.db)
    response = agent.run("Analyze something")
    return response.content
```

### Agent directory layout

```
watchd_agents/
  simple.py              # flat file
  analyzer/              # directory-based agent
    agent.py             # discovered and imported
    system.txt           # agent reads its own files
    _helpers.py          # skipped (underscore prefix)
```

## Key patterns

- APScheduler 3.x (not 4.x), `BlockingScheduler`
- Schedule DSL produces plain data, never imports APScheduler at decoration time
- Agno `SqliteDb` for memory/learning persistence (same db file as run history)
- Model strings: `"provider:model_id"` (e.g. `"anthropic:claude-sonnet-4-5-20250929"`)
- All timestamps are ISO 8601 UTC
- Run IDs are first 12 chars of uuid4 hex

## Stack

- **uv** for tooling (run, sync, venv)
- **hatchling** for build
- **ruff** for lint/format (line-length = 100)
- **pytest** for tests (testpaths = `tests/`)
- **agno** for agent runtime, memory, tools
- **cyclopts** for CLI
- **structlog** for logging
- **sqlalchemy** (required by agno's SqliteDb)
- **anthropic** / **openai** optional via extras
- Python 3.11+
