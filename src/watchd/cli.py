"""CLI commands using cyclopts."""

from __future__ import annotations

import importlib
import json
import sys
from pathlib import Path
from typing import Annotated

import cyclopts

app = cyclopts.App(name="watchd", help="Schedule, run, and track AI agents with zero infra.")

_DEFAULT_APP_LOCATIONS = ["app:app", "main:app", "watchd_app:app"]


def _resolve_app(app_path: str | None):
    """Resolve a Watchd app instance from module:variable notation."""
    from watchd.app import Watchd

    candidates = [app_path] if app_path else _DEFAULT_APP_LOCATIONS

    # Ensure cwd is on sys.path for module discovery
    cwd = str(Path.cwd())
    if cwd not in sys.path:
        sys.path.insert(0, cwd)

    for candidate in candidates:
        if ":" not in candidate:
            raise cyclopts.ValidationError(f"Expected module:variable format, got: {candidate}")
        module_path, var_name = candidate.rsplit(":", 1)
        try:
            module = importlib.import_module(module_path)
        except ImportError:
            if app_path:
                raise
            continue
        obj = getattr(module, var_name, None)
        if isinstance(obj, Watchd):
            return obj
        if app_path:
            raise cyclopts.ValidationError(
                f"'{var_name}' in '{module_path}' is not a Watchd instance"
            )

    raise cyclopts.ValidationError(
        "No Watchd app found. Use --app module:variable or create app.py with `app = Watchd()`"
    )


@app.command
def start(
    *,
    app_path: Annotated[str | None, cyclopts.Parameter(name="--app")] = None,
):
    """Start the scheduler and run agents on their schedules."""
    watchd = _resolve_app(app_path)
    watchd.start()


@app.command
def run(
    agent_name: str,
    *,
    app_path: Annotated[str | None, cyclopts.Parameter(name="--app")] = None,
):
    """Run a single agent immediately."""
    watchd = _resolve_app(app_path)
    result = watchd.run(agent_name)
    _print_run(result)


@app.command(name="list")
def list_agents(
    *,
    app_path: Annotated[str | None, cyclopts.Parameter(name="--app")] = None,
):
    """List all registered agents and their schedules."""
    watchd = _resolve_app(app_path)
    if not watchd.agents:
        print("No agents registered.")
        return
    print(f"{'Agent':<25} {'Schedule':<30} {'Retries'}")
    print("-" * 65)
    for agent in watchd.agents.values():
        schedule = str(agent.schedule) if agent.schedule else "manual"
        print(f"{agent.name:<25} {schedule:<30} {agent.retries}")


@app.command
def history(
    agent_name: str | None = None,
    *,
    limit: int = 20,
    app_path: Annotated[str | None, cyclopts.Parameter(name="--app")] = None,
):
    """Show run history for an agent."""
    watchd = _resolve_app(app_path)
    watchd.store.init()

    if agent_name:
        runs = watchd.store.get_runs(agent_name, limit=limit)
    else:
        # Show runs for all agents
        runs = []
        for name in watchd.agents:
            runs.extend(watchd.store.get_runs(name, limit=limit))
        runs.sort(key=lambda r: r.started_at or "", reverse=True)
        runs = runs[:limit]

    if not runs:
        print("No runs found.")
        return

    print(f"{'ID':<14} {'Agent':<20} {'Status':<10} {'Duration':<12} {'Started'}")
    print("-" * 80)
    for r in runs:
        duration = f"{r.duration_ms:.0f}ms" if r.duration_ms else "-"
        started = r.started_at.strftime("%Y-%m-%d %H:%M:%S") if r.started_at else "-"
        print(f"{r.id:<14} {r.agent:<20} {r.status:<10} {duration:<12} {started}")


@app.command
def state(
    agent_name: str,
    *,
    app_path: Annotated[str | None, cyclopts.Parameter(name="--app")] = None,
):
    """Show persisted state for an agent."""
    watchd = _resolve_app(app_path)
    watchd.store.init()
    data = watchd.store.get_state(agent_name)
    if not data:
        print(f"No state for agent '{agent_name}'.")
        return
    print(json.dumps(data, indent=2, default=str))


def _print_run(r):
    duration = f"{r.duration_ms:.0f}ms" if r.duration_ms else "-"
    print(f"[{r.status}] {r.agent} ({r.id}) in {duration}")
    if r.result:
        print(f"  result: {r.result[:200]}")
    if r.error:
        print(f"  error: {r.error}")


def main():
    app()


if __name__ == "__main__":
    main()
