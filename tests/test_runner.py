import os
import tempfile

import pytest

from watchd.agent import Agent
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


def test_execute_success(store):
    store.sync_agent("test")

    def my_fn(ctx):
        return "hello"

    agent = Agent(name="test", fn=my_fn, schedule=None)
    run = execute_agent(agent, store)
    assert run.status == "success"
    assert run.result == "hello"
    assert run.duration_ms is not None
    assert run.duration_ms >= 0


def test_execute_error(store):
    store.sync_agent("test")

    def failing_fn(ctx):
        raise ValueError("boom")

    agent = Agent(name="test", fn=failing_fn, schedule=None)
    run = execute_agent(agent, store)
    assert run.status == "error"
    assert "boom" in run.error


def test_execute_baseexception_still_updates_run(store):
    store.sync_agent("test")

    def interrupted_fn(ctx):
        raise KeyboardInterrupt()

    agent = Agent(name="test", fn=interrupted_fn, schedule=None)
    with pytest.raises(KeyboardInterrupt):
        execute_agent(agent, store)

    runs = store.get_runs("test")
    assert len(runs) == 1
    assert runs[0].status == "error"
    assert runs[0].duration_ms is not None


def test_execute_persists_state(store):
    store.sync_agent("test")

    def stateful_fn(ctx):
        count = ctx.state.get("count", 0) + 1
        ctx.state["count"] = count
        return f"count={count}"

    agent = Agent(name="test", fn=stateful_fn, schedule=None)

    run1 = execute_agent(agent, store)
    assert run1.result == "count=1"

    run2 = execute_agent(agent, store)
    assert run2.result == "count=2"

    state = store.get_state("test")
    assert state["count"] == 2


def test_execute_retry(store):
    store.sync_agent("test")
    call_count = 0

    def flaky_fn(ctx):
        nonlocal call_count
        call_count += 1
        if call_count < 3:
            raise RuntimeError("not yet")
        return "ok"

    agent = Agent(name="test", fn=flaky_fn, schedule=None, retries=2)
    run = execute_agent(agent, store)
    assert run.status == "success"
    assert run.result == "ok"
    assert call_count == 3


def test_execute_retry_exhausted(store):
    store.sync_agent("test")

    def always_fail(ctx):
        raise RuntimeError("nope")

    agent = Agent(name="test", fn=always_fail, schedule=None, retries=1)
    run = execute_agent(agent, store)
    assert run.status == "error"
    assert "nope" in run.error


def test_run_persisted_to_db(store):
    store.sync_agent("test")
    agent = Agent(name="test", fn=lambda ctx: "ok", schedule=None)
    execute_agent(agent, store)
    runs = store.get_runs("test")
    assert len(runs) == 1
    assert runs[0].status == "success"
