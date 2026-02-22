import subprocess
import sys

import pytest


@pytest.fixture
def app_dir(tmp_path):
    """Create a temp dir with a minimal watchd app."""
    app_file = tmp_path / "app.py"
    app_file.write_text(
        """
from watchd import Watchd, every

app = Watchd(db="./test.db")

@app.agent(schedule=every.seconds(5))
def hello(ctx):
    count = ctx.state.get("count", 0) + 1
    ctx.state["count"] = count
    return f"count={count}"

@app.agent(schedule=every.hour, name="checker")
def check_things(ctx):
    return "checked"
"""
    )
    return tmp_path


def _run_cli(app_dir, *args):
    result = subprocess.run(
        [sys.executable, "-m", "watchd.cli", *args],
        capture_output=True,
        text=True,
        cwd=str(app_dir),
    )
    return result


def test_cli_list(app_dir):
    r = _run_cli(app_dir, "list")
    assert r.returncode == 0
    assert "hello" in r.stdout
    assert "checker" in r.stdout


def test_cli_run(app_dir):
    r = _run_cli(app_dir, "run", "hello")
    assert r.returncode == 0
    assert "success" in r.stdout
    assert "count=1" in r.stdout


def test_cli_run_twice_state_persists(app_dir):
    _run_cli(app_dir, "run", "hello")
    r = _run_cli(app_dir, "run", "hello")
    assert "count=2" in r.stdout


def test_cli_history(app_dir):
    _run_cli(app_dir, "run", "hello")
    r = _run_cli(app_dir, "history", "hello")
    assert r.returncode == 0
    assert "success" in r.stdout


def test_cli_state(app_dir):
    _run_cli(app_dir, "run", "hello")
    r = _run_cli(app_dir, "state", "hello")
    assert r.returncode == 0
    assert '"count": 1' in r.stdout
