"""CLI commands using cyclopts."""

from __future__ import annotations

import hashlib
import importlib.metadata
import json
import sys
from pathlib import Path

import cyclopts

from watchd.config import load_config
from watchd.discovery import discover_agents

app = cyclopts.App(
    name="watchd",
    help="Schedule, run, and track AI agents with zero infra.",
    version=importlib.metadata.version("watchd"),
)

_TOML_TEMPLATE = """\
[watchd]
db = "./watchd.db"
agents_dir = "watchd_agents"
# log_level = "info"
# timezone = "UTC"
"""

_AGENT_TEMPLATE = """\
from watchd import agent, watch


@agent(every="1h")
def {name}(ctx):
    \"\"\"TODO: describe what this agent does.\"\"\"
    ctx.log.info("running")
    return "ok"
"""


def _resolve_from_config():
    """Load config, discover agents, build a Watchd instance."""
    from watchd.app import Watchd

    config = load_config()
    toml_exists = (Path.cwd() / "watchd.toml").exists()
    agents_dir = Path.cwd() / config.agents_dir

    if toml_exists and not agents_dir.is_dir():
        print(
            f"Agents directory '{config.agents_dir}' not found. Run 'watchd init'.", file=sys.stderr
        )
        sys.exit(1)

    if not agents_dir.is_dir():
        return None

    agents = discover_agents(agents_dir)
    if not agents:
        if toml_exists:
            print(
                f"No agents found in '{config.agents_dir}/'. Create one with 'watchd new <name>'.",
                file=sys.stderr,
            )
            sys.exit(1)
        return None

    w = Watchd(db=config.db, log_level=config.log_level, timezone=config.timezone)
    w.agents.update(agents)
    return w


def _resolve():
    """Resolve agents from config + discovery."""
    watchd = _resolve_from_config()
    if watchd:
        return watchd
    raise cyclopts.ValidationError("No agents found. Run 'watchd init' to get started.")


@app.command
def init():
    """Create watchd.toml and watchd_agents/ with an example agent."""
    toml_path = Path.cwd() / "watchd.toml"
    agents_dir = Path.cwd() / "watchd_agents"

    if toml_path.exists():
        print(f"Already exists: {toml_path.name}")
    else:
        toml_path.write_text(_TOML_TEMPLATE)
        print(f"Created {toml_path.name}")

    agents_dir.mkdir(exist_ok=True)
    example = agents_dir / "example.py"
    if example.exists():
        print(f"Already exists: {example.relative_to(Path.cwd())}")
    else:
        example.write_text(_AGENT_TEMPLATE.format(name="example"))
        print(f"Created {example.relative_to(Path.cwd())}")

    print("\nNext: watchd list, watchd run example, watchd up")


@app.command
def new(name: str):
    """Scaffold a new agent file in the agents directory."""
    if not name.isidentifier():
        print(f"Invalid agent name: '{name}'. Must be a valid Python identifier.", file=sys.stderr)
        sys.exit(1)

    config = load_config()
    agents_dir = Path.cwd() / config.agents_dir
    agents_dir.mkdir(exist_ok=True)

    filepath = (agents_dir / f"{name}.py").resolve()
    if not filepath.is_relative_to(agents_dir.resolve()):
        print(f"Invalid agent name: '{name}'.", file=sys.stderr)
        sys.exit(1)

    if filepath.exists():
        print(f"Already exists: {filepath.relative_to(Path.cwd())}")
        return
    filepath.write_text(_AGENT_TEMPLATE.format(name=name))
    print(f"Created {filepath.relative_to(Path.cwd())}")


@app.command
def up():
    """Discover agents and start the scheduler."""
    watchd = _resolve()
    watchd.start()


@app.command
def run(agent_name: str):
    """Run a single agent immediately."""
    watchd = _resolve()
    result = watchd.run(agent_name)
    _print_run(result)


@app.command(name="list")
def list_agents():
    """List all registered agents and their schedules."""
    watchd = _resolve()
    if not watchd.agents:
        print("No agents registered.")
        return
    print(f"{'Agent':<25} {'Schedule':<30} {'Retries'}")
    print("-" * 65)
    for a in watchd.agents.values():
        schedule = str(a.schedule) if a.schedule else "manual"
        print(f"{a.name:<25} {schedule:<30} {a.retries}")


@app.command
def history(agent_name: str | None = None, *, limit: int = 20):
    """Show run history for an agent."""
    watchd = _resolve()
    watchd.store.init()

    if agent_name:
        runs = watchd.store.get_runs(agent_name, limit=limit)
    else:
        runs = watchd.store.get_all_runs(limit=limit)

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
def logs(agent_name: str | None = None, *, run_id: str | None = None, limit: int = 5):
    """Show captured output from agent runs."""
    if not run_id and not agent_name:
        print("Provide an agent name or --run-id.", file=sys.stderr)
        sys.exit(1)

    watchd = _resolve()
    watchd.store.init()

    if run_id:
        r = watchd.store.get_run(run_id)
        if not r:
            print(f"Run '{run_id}' not found.")
            return
        _print_run_detail(r)
    else:
        runs = watchd.store.get_runs(agent_name, limit=limit)
        if not runs:
            print(f"No runs found for '{agent_name}'.")
            return
        for r in runs:
            _print_run_detail(r)
            print()


@app.command
def state(agent_name: str):
    """Show persisted state for an agent."""
    watchd = _resolve()
    watchd.store.init()
    data = watchd.store.get_state(agent_name)
    if not data:
        print(f"No state for agent '{agent_name}'.")
        return
    print(json.dumps(data, indent=2, default=str))


@app.command(name="watch")
def watch_target(
    target: str,
    *,
    every: str = "5m",
    cmd: bool = False,
    mode: str = "full",
    notify: str = "log",
    once: bool = False,
):
    """Watch a URL, file, or command output for changes.

    Examples:
      watchd watch https://example.com --every 5m
      watchd watch /var/log/app.log --every 30s --mode tail
      watchd watch --cmd "df -h /" --every 1m
      watchd watch https://example.com --once
    """
    from watchd import watch as w
    from watchd.app import Watchd
    from watchd.registry import agent as register_agent, clear_registry, get_registry

    def _watch_fn(ctx):
        if cmd:
            return w.command(target, ctx=ctx)
        if target.startswith(("http://", "https://")):
            return w.url(target, ctx=ctx)
        return w.file(target, ctx=ctx, mode=mode)

    def _watcher(ctx):
        change = _watch_fn(ctx)
        if change is None:
            ctx.log.info("no_change", target=target)
            return "no change"
        ctx.notify(f"Change detected in {target}: {change.summary}", channel=notify)
        return change.summary

    name = f"watch-{hashlib.md5(target.encode()).hexdigest()[:8]}"

    db_dir = Path("~/.local/share/watchd").expanduser()
    db_dir.mkdir(parents=True, exist_ok=True)
    db_path = str(db_dir / "watchd.db")

    watchd = Watchd(db=db_path)
    clear_registry()
    register_agent(every=every, name=name)(_watcher)
    watchd.agents.update(get_registry())

    if once:
        result = watchd.run(name)
        _print_run(result)
    else:
        print(f"Watching {target} every {every} (notify: {notify})")
        watchd.start()


@app.command
def setup():
    """First-time server setup: clone, install deps, create systemd service."""
    from watchd.deploy import setup as run_setup

    run_setup(load_config())


@app.command
def deploy():
    """Deploy latest code to server (git pull + restart)."""
    from watchd.deploy import deploy as run_deploy

    run_deploy(load_config())


@app.command
def install():
    """Install as a systemd user service on this machine."""
    from watchd.deploy import install as run_install

    run_install(load_config())


@app.command
def uninstall():
    """Remove the watchd systemd user service."""
    from watchd.deploy import uninstall as run_uninstall

    run_uninstall(load_config())


def _print_run(r):
    duration = f"{r.duration_ms:.0f}ms" if r.duration_ms else "-"
    print(f"[{r.status}] {r.agent} ({r.id}) in {duration}")
    if r.result:
        print(f"  result: {r.result[:200]}")
    if r.error:
        print(f"  error: {r.error}")


def _print_run_detail(r):
    duration = f"{r.duration_ms:.0f}ms" if r.duration_ms else "-"
    started = r.started_at.strftime("%Y-%m-%d %H:%M:%S") if r.started_at else "-"
    print(f"--- {r.id} [{r.status}] {started} ({duration}) ---")
    if r.result:
        print(f"result: {r.result}")
    if r.output:
        print(r.output)
    if r.error:
        print(f"error: {r.error}")


def main():
    app()


if __name__ == "__main__":
    main()
