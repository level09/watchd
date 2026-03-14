import os
import tempfile

import pytest

from watchd.app import Watchd
from watchd.discovery import AgentEntry
from watchd.schedule import every


@pytest.fixture
def app():
    fd, path = tempfile.mkstemp(suffix=".db")
    os.close(fd)
    w = Watchd(db=path)
    yield w
    w.store.close()
    os.unlink(path)


def test_run_full_control(app):
    app.agents["simple"] = AgentEntry(name="simple", schedule=None, run_fn=lambda: "result")
    run = app.run("simple")
    assert run.status == "success"
    assert run.result == "result"


def test_run_unknown_agent(app):
    with pytest.raises(KeyError, match="not_real"):
        app.run("not_real")


def test_multiple_agents(app):
    app.agents["a"] = AgentEntry(name="a", schedule=every.hour, run_fn=lambda: "a")
    app.agents["b"] = AgentEntry(name="b", schedule=every.day.at("12:00"), run_fn=lambda: "b")

    assert len(app.agents) == 2
    assert app.run("a").result == "a"
    assert app.run("b").result == "b"
