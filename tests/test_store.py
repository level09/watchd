import os
import tempfile
from datetime import datetime, timezone

import pytest

from watchd.store import Run, Store


@pytest.fixture
def store():
    fd, path = tempfile.mkstemp(suffix=".db")
    os.close(fd)
    s = Store(path)
    s.init()
    yield s
    s.close()
    os.unlink(path)


def test_init_creates_tables(store):
    tables = store.conn.execute(
        "SELECT name FROM sqlite_master WHERE type='table' ORDER BY name"
    ).fetchall()
    names = {r["name"] for r in tables}
    assert "agents" in names
    assert "runs" in names
    assert "agent_state" in names


def test_sync_agent(store):
    store.sync_agent("test_agent", "every 1h", 2)
    agents = store.get_all_agents()
    assert len(agents) == 1
    assert agents[0]["name"] == "test_agent"
    assert agents[0]["retries"] == 2


def test_sync_agent_upsert(store):
    store.sync_agent("test_agent", "every 1h", 0)
    store.sync_agent("test_agent", "every 2h", 3)
    agents = store.get_all_agents()
    assert len(agents) == 1
    assert agents[0]["schedule"] == "every 2h"
    assert agents[0]["retries"] == 3


def test_save_and_get_run(store):
    store.sync_agent("agent1")
    now = datetime.now(timezone.utc)
    run = Run(id="abc123", agent="agent1", status="success", started_at=now, finished_at=now)
    store.save_run(run)
    runs = store.get_runs("agent1")
    assert len(runs) == 1
    assert runs[0].id == "abc123"
    assert runs[0].status == "success"


def test_update_run(store):
    store.sync_agent("agent1")
    now = datetime.now(timezone.utc)
    run = Run(id="xyz", agent="agent1", status="running", started_at=now)
    store.save_run(run)
    run.status = "success"
    run.finished_at = now
    run.duration_ms = 150.0
    store.update_run(run)
    runs = store.get_runs("agent1")
    assert runs[0].status == "success"
    assert runs[0].duration_ms == 150.0


def test_state_get_set(store):
    store.sync_agent("agent1")
    store.set_state("agent1", "count", 42)
    store.set_state("agent1", "name", "test")
    state = store.get_state("agent1")
    assert state["count"] == 42
    assert state["name"] == "test"


def test_state_overwrite(store):
    store.sync_agent("agent1")
    store.set_state("agent1", "count", 1)
    store.set_state("agent1", "count", 2)
    state = store.get_state("agent1")
    assert state["count"] == 2


def test_state_bulk(store):
    store.sync_agent("agent1")
    store.set_state_bulk("agent1", {"a": 1, "b": "hello", "c": [1, 2, 3]})
    state = store.get_state("agent1")
    assert state == {"a": 1, "b": "hello", "c": [1, 2, 3]}
