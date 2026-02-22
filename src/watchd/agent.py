"""Agent, AgentContext, and StateProxy."""

from __future__ import annotations

from collections.abc import MutableMapping
from dataclasses import dataclass
from typing import TYPE_CHECKING, Callable

if TYPE_CHECKING:
    from watchd.schedule import Schedule
    from watchd.store import Run, Store


@dataclass
class Agent:
    name: str
    fn: Callable
    schedule: Schedule | None
    retries: int = 0


class StateProxy(MutableMapping):
    """Dict-like proxy that reads/writes agent state to SQLite.

    Lazy-loads on first access. Writes through on __setitem__.
    Tracks dirty keys for bulk flush.
    """

    def __init__(self, store: Store, agent_name: str):
        self._store = store
        self._agent = agent_name
        self._data: dict | None = None
        self._dirty: dict[str, object] = {}
        self._deleted_keys: set[str] = set()

    def _load(self):
        if self._data is None:
            self._data = self._store.get_state(self._agent)

    def __getitem__(self, key):
        self._load()
        return self._data[key]

    def __setitem__(self, key, value):
        self._load()
        self._data[key] = value
        self._dirty[key] = value

    def __delitem__(self, key):
        self._load()
        del self._data[key]
        self._deleted_keys.add(key)
        self._dirty.pop(key, None)

    def __iter__(self):
        self._load()
        return iter(self._data)

    def __len__(self):
        self._load()
        return len(self._data)

    def flush(self):
        if self._data is None:
            return
        if self._deleted_keys:
            self._store.delete_state_keys(self._agent, self._deleted_keys)
            self._deleted_keys.clear()
        if self._dirty:
            self._store.set_state_bulk(self._agent, self._dirty)
            self._dirty.clear()


class AgentContext:
    """Passed to the decorated function at runtime."""

    def __init__(self, agent_name: str, run_id: str, store: Store, log):
        self.agent_name = agent_name
        self.run_id = run_id
        self.store = store
        self.log = log
        self._state: StateProxy | None = None

    @property
    def state(self) -> StateProxy:
        if self._state is None:
            self._state = StateProxy(self.store, self.agent_name)
        return self._state

    @property
    def history(self) -> list[Run]:
        return self.store.get_runs(self.agent_name, limit=10)
