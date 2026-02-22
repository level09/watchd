import os
import tempfile

import pytest

from watchd import Watchd, every


@pytest.fixture
def app():
    fd, path = tempfile.mkstemp(suffix=".db")
    os.close(fd)
    w = Watchd(db=path)
    yield w
    w.store.close()
    os.unlink(path)


def test_agent_decorator_registers(app):
    @app.agent(schedule=every.hour)
    def my_agent(ctx):
        return "done"

    assert "my_agent" in app.agents
    assert app.agents["my_agent"].schedule == every.hour


def test_agent_custom_name(app):
    @app.agent(schedule=every.minutes(5), name="custom_name")
    def whatever(ctx):
        return "ok"

    assert "custom_name" in app.agents
    assert "whatever" not in app.agents


def test_run_immediate(app):
    @app.agent()
    def simple(ctx):
        ctx.state["ran"] = True
        return "result"

    run = app.run("simple")
    assert run.status == "success"
    assert run.result == "result"

    state = app.store.get_state("simple")
    assert state["ran"] is True


def test_run_unknown_agent(app):
    with pytest.raises(KeyError, match="not_real"):
        app.run("not_real")


def test_multiple_agents(app):
    @app.agent(schedule=every.hour)
    def agent_a(ctx):
        return "a"

    @app.agent(schedule=every.day.at("12:00"))
    def agent_b(ctx):
        return "b"

    assert len(app.agents) == 2
    assert app.run("agent_a").result == "a"
    assert app.run("agent_b").result == "b"
