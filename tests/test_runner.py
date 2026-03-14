import os
import tempfile

import pytest

from watchd.registry import AgentEntry
from watchd.runner import execute_agent
from watchd.store import Store


@pytest.fixture
def store():
    fd, path = tempfile.mkstemp(suffix=".db")
    os.close(fd)
    s = Store(path)
    s.init()
    yield s
    s.close()
    os.unlink(path)


def test_execute_success_full_mode(store):
    """Full mode: function takes ctx, returns a value."""
    store.sync_agent("test")

    def my_fn(ctx):
        return "hello"

    entry = AgentEntry(name="test", fn=my_fn, schedule=None)
    run = execute_agent(entry, store)
    assert run.status == "success"
    assert run.result == "hello"
    assert run.duration_ms is not None
    assert run.duration_ms >= 0


def test_execute_success_simple_mode(store):
    """Simple mode: no-arg function returns a prompt string (no model configured)."""
    store.sync_agent("test")

    def my_fn():
        return "analyze this"

    entry = AgentEntry(name="test", fn=my_fn, schedule=None)
    run = execute_agent(entry, store)
    assert run.status == "success"
    assert run.result == "analyze this"


def test_execute_error(store):
    store.sync_agent("test")

    def failing_fn(ctx):
        raise ValueError("boom")

    entry = AgentEntry(name="test", fn=failing_fn, schedule=None)
    run = execute_agent(entry, store)
    assert run.status == "error"
    assert "boom" in run.error


def test_execute_baseexception_still_updates_run(store):
    store.sync_agent("test")

    def interrupted_fn(ctx):
        raise KeyboardInterrupt()

    entry = AgentEntry(name="test", fn=interrupted_fn, schedule=None)
    with pytest.raises(KeyboardInterrupt):
        execute_agent(entry, store)

    runs = store.get_runs("test")
    assert len(runs) == 1
    assert runs[0].status == "error"
    assert runs[0].duration_ms is not None


def test_run_persisted_to_db(store):
    store.sync_agent("test")
    entry = AgentEntry(name="test", fn=lambda ctx: "ok", schedule=None)
    execute_agent(entry, store)
    runs = store.get_runs("test")
    assert len(runs) == 1
    assert runs[0].status == "success"


def test_stdout_captured(store):
    store.sync_agent("test")

    def chatty_fn(ctx):
        print("hello from agent")
        return "done"

    entry = AgentEntry(name="test", fn=chatty_fn, schedule=None)
    run = execute_agent(entry, store)
    assert run.status == "success"
    assert "hello from agent" in run.output


def test_no_output_is_none(store):
    store.sync_agent("test")
    entry = AgentEntry(name="test", fn=lambda ctx: "quiet", schedule=None)
    run = execute_agent(entry, store)
    assert run.output is None
