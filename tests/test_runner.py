import os
import tempfile

import pytest

from watchd.discovery import AgentEntry
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


def test_execute_full_control(store):
    store.sync_agent("test")

    entry = AgentEntry(name="test", schedule=None, run_fn=lambda: "hello")
    run = execute_agent(entry, store)
    assert run.status == "success"
    assert run.result == "hello"
    assert run.duration_ms >= 0


def test_execute_error(store):
    store.sync_agent("test")

    def failing():
        raise ValueError("boom")

    entry = AgentEntry(name="test", schedule=None, run_fn=failing)
    run = execute_agent(entry, store)
    assert run.status == "error"
    assert "boom" in run.error


def test_execute_baseexception_still_updates_run(store):
    store.sync_agent("test")

    def interrupted():
        raise KeyboardInterrupt()

    entry = AgentEntry(name="test", schedule=None, run_fn=interrupted)
    with pytest.raises(KeyboardInterrupt):
        execute_agent(entry, store)

    runs = store.get_runs("test")
    assert len(runs) == 1
    assert runs[0].status == "error"
    assert runs[0].duration_ms is not None


def test_run_persisted_to_db(store):
    store.sync_agent("test")
    entry = AgentEntry(name="test", schedule=None, run_fn=lambda: "ok")
    execute_agent(entry, store)
    runs = store.get_runs("test")
    assert len(runs) == 1
    assert runs[0].status == "success"


def test_stdout_captured(store):
    store.sync_agent("test")

    def chatty():
        print("hello from agent")
        return "done"

    entry = AgentEntry(name="test", schedule=None, run_fn=chatty)
    run = execute_agent(entry, store)
    assert run.status == "success"
    assert "hello from agent" in run.output


def test_no_output_is_none(store):
    store.sync_agent("test")
    entry = AgentEntry(name="test", schedule=None, run_fn=lambda: "quiet")
    run = execute_agent(entry, store)
    assert run.output is None


def test_result_with_content_attribute(store):
    """If run_fn returns object with .content, use that."""
    store.sync_agent("test")

    class FakeResponse:
        content = "from agno"

    entry = AgentEntry(name="test", schedule=None, run_fn=lambda: FakeResponse())
    run = execute_agent(entry, store)
    assert run.result == "from agno"
