"""Global agent registry. Module-level @agent() decorator for file-based discovery."""

from __future__ import annotations

import structlog

from watchd.agent import Agent
from watchd.schedule import Schedule, parse_schedule

log = structlog.get_logger()

_registry: dict[str, Agent] = {}


def agent(
    every: str | None = None,
    schedule: Schedule | None = None,
    name: str | None = None,
    retries: int = 0,
):
    """Decorator to register an agent in the global registry.

    Schedule via string: @agent(every="5m")
    Schedule via DSL:    @agent(schedule=every.minutes(5))
    """

    def decorator(fn):
        agent_name = name or fn.__name__

        resolved = None
        if every:
            resolved = parse_schedule(every)
        elif schedule:
            resolved = schedule

        if agent_name in _registry:
            log.warning("duplicate_agent_name", name=agent_name, replacing=_registry[agent_name].fn)
        _registry[agent_name] = Agent(name=agent_name, fn=fn, schedule=resolved, retries=retries)
        return fn

    return decorator


def get_registry() -> dict[str, Agent]:
    return _registry


def clear_registry():
    _registry.clear()
