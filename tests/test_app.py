import os
import tempfile

import pytest

from watchd.app import Watchd
from watchd.registry import AgentEntry, clear_registry
from watchd.schedule import every


@pytest.fixture
def app():
    fd, path = tempfile.mkstemp(suffix=".db")
    os.close(fd)
    w = Watchd(db=path)
    clear_registry()
    yield w
    w.store.close()
    os.unlink(path)


def test_register_and_run(app):
    def my_agent(ctx):
        return "done"

    app.agents["my_agent"] = AgentEntry(name="my_agent", fn=my_agent, schedule=every.hour)
    assert "my_agent" in app.agents
    assert app.agents["my_agent"].schedule == every.hour


def test_run_immediate(app):
    def simple(ctx):
        return "result"

    app.agents["simple"] = AgentEntry(name="simple", fn=simple, schedule=None)
    run = app.run("simple")
    assert run.status == "success"
    assert run.result == "result"


def test_run_unknown_agent(app):
    with pytest.raises(KeyError, match="not_real"):
        app.run("not_real")


def test_multiple_agents(app):
    def agent_a(ctx):
        return "a"

    def agent_b(ctx):
        return "b"

    app.agents["agent_a"] = AgentEntry(name="agent_a", fn=agent_a, schedule=every.hour)
    app.agents["agent_b"] = AgentEntry(name="agent_b", fn=agent_b, schedule=every.day.at("12:00"))

    assert len(app.agents) == 2
    assert app.run("agent_a").result == "a"
    assert app.run("agent_b").result == "b"
