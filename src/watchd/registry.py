"""Global agent registry. @agent() decorator for file-based discovery."""

from __future__ import annotations

from dataclasses import dataclass, field
from typing import Any, Callable

import structlog

from watchd.schedule import Schedule, parse_schedule

log = structlog.get_logger()

_registry: dict[str, AgentEntry] = {}


@dataclass
class AgentEntry:
    name: str
    fn: Callable
    schedule: Schedule | None = None
    retries: int = 0
    model: str | None = None
    tools: list[Any] = field(default_factory=list)
    instructions: list[str] = field(default_factory=list)
    learning: bool = False
    output_schema: Any = None
    description: str | None = None


def agent(
    every: str | None = None,
    schedule: Schedule | None = None,
    name: str | None = None,
    retries: int = 0,
    model: str | None = None,
    tools: list | None = None,
    instructions: list[str] | str | None = None,
    learning: bool = False,
    output_schema: Any = None,
):
    """Decorator to register an agent in the global registry.

    Simple mode: function returns a prompt string, watchd builds the Agno Agent.
    Full mode: function receives ctx and builds its own Agno Agent.
    """

    def decorator(fn):
        agent_name = name or fn.__name__

        resolved = None
        if every:
            resolved = parse_schedule(every)
        elif schedule:
            resolved = schedule

        instr = instructions
        if isinstance(instr, str):
            instr = [instr]

        if agent_name in _registry:
            log.warning("duplicate_agent_name", name=agent_name, replacing=_registry[agent_name].fn)

        _registry[agent_name] = AgentEntry(
            name=agent_name,
            fn=fn,
            schedule=resolved,
            retries=retries,
            model=model,
            tools=tools or [],
            instructions=instr or [],
            learning=learning,
            output_schema=output_schema,
            description=fn.__doc__,
        )
        return fn

    return decorator


def get_registry() -> dict[str, AgentEntry]:
    return _registry


def clear_registry():
    _registry.clear()
