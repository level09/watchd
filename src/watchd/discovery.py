"""Agent discovery from a directory of .py files and subdirectories containing agent.py."""

from __future__ import annotations

import importlib.util
import sys
from pathlib import Path

import structlog

from watchd.agent import Agent
from watchd.registry import clear_registry, get_registry

log = structlog.get_logger()


def discover_agents(agents_dir: str | Path) -> dict[str, Agent]:
    """Scan agents_dir for .py files, import them, return registered agents."""
    clear_registry()
    agents_path = Path(agents_dir)
    if not agents_path.is_dir():
        return {}

    # Ensure parent is importable
    parent = str(agents_path.parent)
    if parent not in sys.path:
        sys.path.insert(0, parent)

    dir_name = agents_path.name

    for py_file in sorted(agents_path.glob("*.py")):
        if py_file.name.startswith("_"):
            continue
        module_name = f"{dir_name}.{py_file.stem}"
        spec = importlib.util.spec_from_file_location(module_name, py_file)
        if spec and spec.loader:
            module = importlib.util.module_from_spec(spec)
            sys.modules[module_name] = module
            try:
                spec.loader.exec_module(module)
            except Exception as e:
                log.error("agent_load_failed", file=str(py_file.name), error=str(e))

    for agent_file in sorted(agents_path.glob("*/agent.py")):
        if agent_file.parent.name.startswith("_"):
            continue
        module_name = f"{dir_name}.{agent_file.parent.name}.agent"
        spec = importlib.util.spec_from_file_location(module_name, agent_file)
        if spec and spec.loader:
            module = importlib.util.module_from_spec(spec)
            sys.modules[module_name] = module
            try:
                spec.loader.exec_module(module)
            except Exception as e:
                log.error("agent_load_failed", file=str(agent_file), error=str(e))

    return dict(get_registry())
