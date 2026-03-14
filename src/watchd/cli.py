"""CLI commands using cyclopts."""

from __future__ import annotations

import difflib
import importlib.metadata
import sys
from pathlib import Path
from typing import Annotated

import cyclopts

from watchd.config import load_config
from watchd.discovery import discover_agents
from watchd.output import console, err_console, make_table, print_json, relative_time, status_icon

_json_flag = Annotated[bool, cyclopts.Parameter(name=["--json", "--as-json"])]

app = cyclopts.App(
    name="watchd",
    help="Schedule, run, and track AI agents with zero infra.",
    version=importlib.metadata.version("watchd"),
)

_TOML_TEMPLATE = """\
[watchd]
db = "./watchd.db"
agents_dir = "watchd_agents"
# model = "anthropic:claude-sonnet-4-5-20250929"
# learning = false
# log_level = "info"
# timezone = "UTC"
"""

_AGENT_TEMPLATE = """\
from watchd import agent


@agent(every="1h")
def {name}(ctx):
    \"\"\"TODO: describe what this agent does.\"\"\"
    ctx.log.info("running")
    return "ok"
"""


def _resolve_from_config():
    from watchd.app import Watchd

    config = load_config()
    toml_exists = (Path.cwd() / "watchd.toml").exists()
    agents_dir = Path.cwd() / config.agents_dir

    if toml_exists and not agents_dir.is_dir():
        err_console.print(
            f"Agents directory '{config.agents_dir}' not found. Run [bold]watchd init[/bold]."
        )
        sys.exit(1)

    if not agents_dir.is_dir():
        return None

    agents = discover_agents(agents_dir)
    if not agents:
        if toml_exists:
            err_console.print(
                f"No agents in '{config.agents_dir}/'. "
                "Create one with [bold]watchd new <name>[/bold]."
            )
            sys.exit(1)
        return None

    w = Watchd(db=config.db, log_level=config.log_level, timezone=config.timezone)
    w.agents.update(agents)
    return w


def _resolve():
    watchd = _resolve_from_config()
    if watchd:
        return watchd
    raise cyclopts.ValidationError("No agents found. Run 'watchd init' to get started.")


def _resolve_agent(watchd, agent_name: str):
    if agent_name in watchd.agents:
        return agent_name
    close = difflib.get_close_matches(agent_name, watchd.agents.keys(), n=1, cutoff=0.6)
    hint = f" Did you mean '{close[0]}'?" if close else ""
    err_console.print(f"Unknown agent '{agent_name}'.{hint}")
    sys.exit(1)


@app.command
def init():
    """Create watchd.toml and watchd_agents/ with an example agent."""
    toml_path = Path.cwd() / "watchd.toml"
    agents_dir = Path.cwd() / "watchd_agents"

    if toml_path.exists():
        console.print(f"Already exists: {toml_path.name}")
    else:
        toml_path.write_text(_TOML_TEMPLATE)
        console.print(f"[ok]Created[/ok] {toml_path.name}")

    agents_dir.mkdir(exist_ok=True)
    example = agents_dir / "example.py"
    if example.exists():
        console.print(f"Already exists: {example.relative_to(Path.cwd())}")
    else:
        example.write_text(_AGENT_TEMPLATE.format(name="example"))
        console.print(f"[ok]Created[/ok] {example.relative_to(Path.cwd())}")

    console.print("\nNext: watchd list, watchd run example, watchd up")


@app.command
def new(name: str):
    """Scaffold a new agent file in the agents directory."""
    if not name.isidentifier():
        err_console.print(f"Invalid agent name: '{name}'. Must be a valid Python identifier.")
        sys.exit(1)

    config = load_config()
    agents_dir = Path.cwd() / config.agents_dir
    agents_dir.mkdir(exist_ok=True)

    filepath = (agents_dir / f"{name}.py").resolve()
    if not filepath.is_relative_to(agents_dir.resolve()):
        err_console.print(f"Invalid agent name: '{name}'.")
        sys.exit(1)

    if filepath.exists():
        console.print(f"Already exists: {filepath.relative_to(Path.cwd())}")
        return
    filepath.write_text(_AGENT_TEMPLATE.format(name=name))
    console.print(f"[ok]Created[/ok] {filepath.relative_to(Path.cwd())}")


@app.command
def up():
    """Discover agents and start the scheduler."""
    watchd = _resolve()
    n = len(watchd.agents)
    console.print(f"Starting scheduler with {n} agent{'s' if n != 1 else ''}...")
    watchd.start()


@app.command
def run(agent_name: str, *, json: _json_flag = False):
    """Run a single agent immediately."""
    watchd = _resolve()
    _resolve_agent(watchd, agent_name)
    with console.status(f"Running {agent_name}..."):
        result = watchd.run(agent_name)
    if json:
        print_json(_run_to_dict(result))
    else:
        _print_run(result)
    if result.status == "error":
        sys.exit(1)


@app.command(name="list")
def list_agents(*, json: _json_flag = False):
    """List all registered agents and their schedules."""
    watchd = _resolve()
    if not watchd.agents:
        console.print("No agents registered.")
        return

    if json:
        data = [
            {
                "name": a.name,
                "schedule": str(a.schedule) if a.schedule else None,
                "retries": a.retries,
                "model": a.model,
                "learning": a.learning,
            }
            for a in watchd.agents.values()
        ]
        print_json(data)
        return

    table = make_table("Agent", "Schedule", "Model", "Learning")
    for a in watchd.agents.values():
        schedule = str(a.schedule) if a.schedule else "[dim]manual[/dim]"
        model = a.model or "[dim]default[/dim]"
        learning = "[ok]on[/ok]" if a.learning else "[dim]off[/dim]"
        table.add_row(a.name, schedule, model, learning)
    console.print(table)


@app.command
def status(agent_name: str | None = None, *, json: _json_flag = False):
    """Dashboard: agents with their last run status."""
    watchd = _resolve()
    watchd.store.init()

    agents = list(watchd.agents.values())
    if agent_name:
        _resolve_agent(watchd, agent_name)
        agents = [a for a in agents if a.name == agent_name]

    rows = []
    for a in agents:
        runs = watchd.store.get_runs(a.name, limit=1)
        last = runs[0] if runs else None
        rows.append({"agent": a, "last_run": last})

    if json:
        data = []
        for row in rows:
            entry = {
                "name": row["agent"].name,
                "schedule": str(row["agent"].schedule) if row["agent"].schedule else None,
            }
            if row["last_run"]:
                entry["last_run"] = _run_to_dict(row["last_run"])
            data.append(entry)
        print_json(data)
        return

    table = make_table("", "Agent", "Schedule", "Last Run", "Duration", "When")
    for row in rows:
        a = row["agent"]
        r = row["last_run"]
        schedule = str(a.schedule) if a.schedule else "[dim]manual[/dim]"
        if r:
            icon = status_icon(r.status)
            duration = f"{r.duration_ms:.0f}ms" if r.duration_ms else "-"
            when = relative_time(r.started_at)
        else:
            icon = "[dim]-[/dim]"
            duration = "-"
            when = "[dim]never[/dim]"
        table.add_row(icon, a.name, schedule, r.id if r else "-", duration, when)
    console.print(table)


@app.command
def history(agent_name: str | None = None, *, limit: int = 20, json: _json_flag = False):
    """Show run history for an agent."""
    watchd = _resolve()
    watchd.store.init()

    if agent_name:
        _resolve_agent(watchd, agent_name)
        runs = watchd.store.get_runs(agent_name, limit=limit)
    else:
        runs = watchd.store.get_all_runs(limit=limit)

    if not runs:
        console.print("No runs found.")
        return

    if json:
        print_json([_run_to_dict(r) for r in runs])
        return

    table = make_table("", "ID", "Agent", "Status", "Duration", "When")
    for r in runs:
        duration = f"{r.duration_ms:.0f}ms" if r.duration_ms else "-"
        table.add_row(
            status_icon(r.status),
            r.id,
            r.agent,
            r.status,
            duration,
            relative_time(r.started_at),
        )
    console.print(table)


@app.command
def logs(agent_name: str | None = None, *, run_id: str | None = None, limit: int = 5):
    """Show captured output from agent runs."""
    if not run_id and not agent_name:
        err_console.print("Provide an agent name or --run-id.")
        sys.exit(1)

    watchd = _resolve()
    watchd.store.init()

    if run_id:
        r = watchd.store.get_run(run_id)
        if not r:
            err_console.print(f"Run '{run_id}' not found.")
            return
        _print_run_detail(r)
    else:
        _resolve_agent(watchd, agent_name)
        runs = watchd.store.get_runs(agent_name, limit=limit)
        if not runs:
            console.print(f"No runs found for '{agent_name}'.")
            return
        for r in runs:
            _print_run_detail(r)
            console.print()


@app.command
def memory(agent_name: str, *, json: _json_flag = False):
    """Show learned memories for an agent."""
    import json as json_mod
    import sqlite3

    watchd = _resolve()
    _resolve_agent(watchd, agent_name)

    try:
        conn = sqlite3.connect(watchd._db_path)
        conn.row_factory = sqlite3.Row
        rows = conn.execute(
            "SELECT learning_type, content FROM agno_learnings WHERE user_id=? ORDER BY created_at",
            (agent_name,),
        ).fetchall()
        conn.close()
    except sqlite3.OperationalError:
        console.print(
            f"No memories for '{agent_name}' yet. Run the agent first with learning=True."
        )
        return

    if not rows:
        console.print(f"No memories for '{agent_name}' yet.")
        return

    if json:
        data = []
        for r in rows:
            data.append({"type": r["learning_type"], "content": json_mod.loads(r["content"])})
        print_json(data)
        return

    for r in rows:
        content = json_mod.loads(r["content"])
        label = r["learning_type"]

        if label == "user_profile":
            console.print("  [info]profile[/info]")
            for k, v in content.items():
                if v and k not in ("user_id", "agent_id", "team_id", "created_at", "updated_at"):
                    console.print(f"    {k}: {v}")

        elif label == "user_memory":
            memories = content.get("memories", [])
            if memories:
                console.print("  [info]memories[/info]")
                for m in memories:
                    console.print(f"    [dim]-[/dim] {m.get('content', m)}")

        else:
            console.print(f"  [info]{label}[/info]")
            console.print(f"    {json_mod.dumps(content, indent=2, default=str)}")


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


@app.command
def doctor(*, json: _json_flag = False):
    """Check your watchd setup for problems."""
    import importlib.util

    checks = []

    def check(name, ok, detail=""):
        checks.append({"name": name, "ok": ok, "detail": detail})

    v = sys.version_info
    check("Python", v >= (3, 11), f"{v.major}.{v.minor}.{v.micro}")

    try:
        ver = importlib.metadata.version("watchd")
        check("watchd", True, ver)
    except importlib.metadata.PackageNotFoundError:
        check("watchd", False, "not installed as package")

    # Agno
    agno_found = importlib.util.find_spec("agno") is not None
    if agno_found:
        try:
            agno_ver = importlib.metadata.version("agno")
            check("agno", True, agno_ver)
        except importlib.metadata.PackageNotFoundError:
            check("agno", True, "installed")
    else:
        check("agno", False, "not installed (required)")

    toml_path = Path.cwd() / "watchd.toml"
    if toml_path.exists():
        try:
            config = load_config()
            check("Config", True, str(toml_path.name))
        except SystemExit:
            check("Config", False, "invalid TOML")
            config = None
    else:
        check("Config", True, "no watchd.toml (using defaults)")
        config = load_config()

    if config:
        agents_dir = Path.cwd() / config.agents_dir
        if agents_dir.is_dir():
            py_files = list(agents_dir.glob("*.py")) + [
                p / "agent.py"
                for p in agents_dir.iterdir()
                if p.is_dir() and not p.name.startswith("_") and (p / "agent.py").exists()
            ]
            check("Agents dir", True, f"{len(py_files)} file(s) in {config.agents_dir}/")

            agents = discover_agents(agents_dir)
            if agents:
                check("Discovery", True, f"{len(agents)} agent(s) loaded")
                bad = []
                for a in agents.values():
                    if a.schedule:
                        try:
                            a.schedule.to_apscheduler_trigger()
                        except Exception as e:
                            bad.append(f"{a.name}: {e}")
                if bad:
                    check("Schedules", False, "; ".join(bad))
                else:
                    check("Schedules", True, "all valid")
            else:
                check("Discovery", False, f"no agents found in {config.agents_dir}/")
        else:
            check("Agents dir", False, f"{config.agents_dir}/ not found")

        db_path = Path(config.db)
        if db_path.exists():
            try:
                from watchd.store import Store

                store = Store(config.db)
                store.init()
                store.conn.execute("SELECT 1")
                check("Database", True, config.db)
            except Exception as e:
                check("Database", False, str(e))
        else:
            parent = db_path.parent.resolve()
            check("Database", parent.is_dir(), f"{config.db} (will be created)")

    for pkg in ("anthropic", "openai"):
        found = importlib.util.find_spec(pkg) is not None
        check(f"  {pkg}", found, "installed" if found else "not installed (optional)")

    if json:
        print_json(checks)
        return

    for c in checks:
        icon = "[ok]✓[/ok]" if c["ok"] else "[fail]✗[/fail]"
        detail = f"  [dim]{c['detail']}[/dim]" if c["detail"] else ""
        console.print(f"  {icon} {c['name']}{detail}")

    failed = [c for c in checks if not c["ok"] and not c["name"].startswith("  ")]
    if failed:
        sys.exit(1)


def _run_to_dict(r) -> dict:
    return {
        "id": r.id,
        "agent": r.agent,
        "status": r.status,
        "result": r.result,
        "error": r.error,
        "duration_ms": r.duration_ms,
        "started_at": r.started_at.isoformat() if r.started_at else None,
        "finished_at": r.finished_at.isoformat() if r.finished_at else None,
    }


def _print_run(r):
    icon = status_icon(r.status)
    duration = f"{r.duration_ms:.0f}ms" if r.duration_ms else "-"
    console.print(f"{icon} {r.agent} ({r.id}) in {duration}")
    if r.result:
        console.print(f"  result: {r.result[:200]}")
    if r.error:
        console.print(f"  [fail]error:[/fail] {r.error}")


def _print_run_detail(r):
    icon = status_icon(r.status)
    duration = f"{r.duration_ms:.0f}ms" if r.duration_ms else "-"
    when = relative_time(r.started_at)
    console.print(f"{icon} {r.id} [{r.status}] {when} ({duration})")
    if r.result:
        console.print(f"  result: {r.result}")
    if r.output:
        console.print(r.output)
    if r.error:
        console.print(f"  [fail]error:[/fail] {r.error}")


def _load_dotenv():
    env_path = Path.cwd() / ".env"
    if not env_path.is_file():
        return
    import os

    for line in env_path.read_text().splitlines():
        line = line.strip()
        if not line or line.startswith("#") or "=" not in line:
            continue
        key, _, value = line.partition("=")
        key = key.strip()
        value = value.strip().strip("'\"")
        if key and key not in os.environ:
            os.environ[key] = value


def main():
    _load_dotenv()
    try:
        app()
    except KeyboardInterrupt:
        sys.exit(130)


if __name__ == "__main__":
    main()
