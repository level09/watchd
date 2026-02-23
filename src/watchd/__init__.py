"""watchd - Schedule, run, and track AI agents with zero infra."""

__version__ = "0.1.0"

from watchd.app import Watchd
from watchd.registry import agent
from watchd.schedule import every

__all__ = ["Watchd", "agent", "every"]
