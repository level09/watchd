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


def test_get_run_by_id(store):
    store.sync_agent("agent1")
    now = datetime.now(timezone.utc)
    run = Run(id="findme", agent="agent1", status="success", started_at=now)
    store.save_run(run)
    found = store.get_run("findme")
    assert found is not None
    assert found.id == "findme"
    assert store.get_run("nonexistent") is None


def test_get_all_runs(store):
    store.sync_agent("a1")
    store.sync_agent("a2")
    now = datetime.now(timezone.utc)
    store.save_run(Run(id="r1", agent="a1", status="success", started_at=now))
    store.save_run(Run(id="r2", agent="a2", status="error", started_at=now))
    all_runs = store.get_all_runs(limit=10)
    assert len(all_runs) == 2
