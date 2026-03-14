"""watchd - Schedule, run, and track AI agents with zero infra."""

__version__ = "2.0.0"

from watchd.registry import agent
from watchd.schedule import every

__all__ = ["agent", "every"]
