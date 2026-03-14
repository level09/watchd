"""Convention-based agent discovery. No decorators, no registry.

Scans a directory for .py files and subdirectories with agent.py.
An agent file must have at minimum: `schedule` (str).
Plus one of:
  - `agent` (Agno Agent) + `prompt` (str)  -> declarative mode
  - `run` (callable)                        -> full control mode
"""

from __future__ import annotations

import importlib.util
import sys
from dataclasses import dataclass
from pathlib import Path
from typing import Any, Callable

import structlog

from watchd.schedule import Schedule, parse_schedule

log = structlog.get_logger()


@dataclass
class AgentEntry:
    name: str
    schedule: Schedule | None
    # Declarative mode
    agno_agent: Any | None = None
    prompt: str | None = None
    # Full control mode
    run_fn: Callable | None = None

    @property
    def is_declarative(self) -> bool:
        return self.agno_agent is not None and self.prompt is not None


def discover_agents(agents_dir: str | Path) -> dict[str, AgentEntry]:
    """Scan agents_dir for agent files, return discovered agents."""
    agents_path = Path(agents_dir)
    if not agents_path.is_dir():
        return {}

    parent = str(agents_path.parent)
    if parent not in sys.path:
        sys.path.insert(0, parent)

    dir_name = agents_path.name
    agents = {}

    # Flat files: agents_dir/*.py
    for py_file in sorted(agents_path.glob("*.py")):
        if py_file.name.startswith("_"):
            continue
        module_name = f"{dir_name}.{py_file.stem}"
        entry = _load_agent(module_name, py_file, py_file.stem)
        if entry:
            agents[entry.name] = entry

    # Directory agents: agents_dir/*/agent.py
    for agent_file in sorted(agents_path.glob("*/agent.py")):
        if agent_file.parent.name.startswith("_"):
            continue
        module_name = f"{dir_name}.{agent_file.parent.name}.agent"
        entry = _load_agent(module_name, agent_file, agent_file.parent.name)
        if entry:
            agents[entry.name] = entry

    return agents


def _load_agent(module_name: str, filepath: Path, default_name: str) -> AgentEntry | None:
    """Import a module and extract agent convention variables."""
    spec = importlib.util.spec_from_file_location(module_name, filepath)
    if not spec or not spec.loader:
        return None

    module = importlib.util.module_from_spec(spec)
    sys.modules[module_name] = module
    try:
        spec.loader.exec_module(module)
    except Exception as e:
        log.error("agent_load_failed", file=str(filepath.name), error=str(e))
        return None

    # Must have schedule
    schedule_val = getattr(module, "schedule", None)
    if schedule_val is None:
        # No schedule = not a watchd agent, skip silently
        return None

    # Parse schedule
    if isinstance(schedule_val, str):
        try:
            schedule = parse_schedule(schedule_val)
        except ValueError as e:
            log.error("bad_schedule", file=str(filepath.name), error=str(e))
            return None
    elif isinstance(schedule_val, Schedule):
        schedule = schedule_val
    else:
        log.error(
            "bad_schedule", file=str(filepath.name), error="schedule must be a string or Schedule"
        )
        return None

    name = getattr(module, "name", default_name)
    agno_agent = getattr(module, "agent", None)
    prompt = getattr(module, "prompt", None)
    run_fn = getattr(module, "run", None)

    if agno_agent and prompt:
        return AgentEntry(name=name, schedule=schedule, agno_agent=agno_agent, prompt=prompt)
    elif callable(run_fn):
        return AgentEntry(name=name, schedule=schedule, run_fn=run_fn)
    else:
        log.error(
            "incomplete_agent",
            file=str(filepath.name),
            error="need (agent + prompt) or run()",
        )
        return None
