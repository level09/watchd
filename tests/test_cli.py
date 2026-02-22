import subprocess
import sys

import pytest


@pytest.fixture
def app_dir(tmp_path):
    """Create a temp dir with a minimal watchd app (legacy mode)."""
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


@pytest.fixture
def discovery_dir(tmp_path):
    """Create a temp dir with watchd.toml + agent files (new mode)."""
    (tmp_path / "watchd.toml").write_text(
        '[watchd]\ndb = "./test.db"\nagents_dir = "watchd_agents"\n'
    )
    agents = tmp_path / "watchd_agents"
    agents.mkdir()
    (agents / "hello.py").write_text(
        "from watchd import agent, every\n\n"
        "@agent(schedule=every.seconds(5))\n"
        "def hello(ctx):\n"
        "    count = ctx.state.get('count', 0) + 1\n"
        "    ctx.state['count'] = count\n"
        "    return f'count={count}'\n"
    )
    return tmp_path


def _run_cli(cwd, *args):
    result = subprocess.run(
        [sys.executable, "-m", "watchd.cli", *args],
        capture_output=True,
        text=True,
        cwd=str(cwd),
    )
    return result


# --- Legacy mode tests ---


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


# --- Init / new commands ---


def test_cli_init(tmp_path):
    r = _run_cli(tmp_path, "init")
    assert r.returncode == 0
    assert (tmp_path / "watchd.toml").exists()
    assert (tmp_path / "watchd_agents" / "example.py").exists()
    assert "Created watchd.toml" in r.stdout


def test_cli_init_idempotent(tmp_path):
    _run_cli(tmp_path, "init")
    r = _run_cli(tmp_path, "init")
    assert r.returncode == 0
    assert "Already exists" in r.stdout


def test_cli_new(tmp_path):
    _run_cli(tmp_path, "init")
    r = _run_cli(tmp_path, "new", "fetcher")
    assert r.returncode == 0
    assert (tmp_path / "watchd_agents" / "fetcher.py").exists()
    assert "Created" in r.stdout


def test_cli_new_already_exists(tmp_path):
    _run_cli(tmp_path, "init")
    _run_cli(tmp_path, "new", "fetcher")
    r = _run_cli(tmp_path, "new", "fetcher")
    assert "Already exists" in r.stdout


# --- Discovery mode tests ---


def test_discovery_list(discovery_dir):
    r = _run_cli(discovery_dir, "list")
    assert r.returncode == 0
    assert "hello" in r.stdout


def test_discovery_run(discovery_dir):
    r = _run_cli(discovery_dir, "run", "hello")
    assert r.returncode == 0
    assert "success" in r.stdout
    assert "count=1" in r.stdout


def test_discovery_run_state_persists(discovery_dir):
    _run_cli(discovery_dir, "run", "hello")
    r = _run_cli(discovery_dir, "run", "hello")
    assert "count=2" in r.stdout


# --- Logs command ---


def test_cli_logs(app_dir):
    _run_cli(app_dir, "run", "hello")
    r = _run_cli(app_dir, "logs", "hello")
    assert r.returncode == 0
    assert "success" in r.stdout


# --- Version ---


def test_cli_version():
    r = subprocess.run(
        [sys.executable, "-m", "watchd.cli", "--version"],
        capture_output=True,
        text=True,
    )
    assert r.returncode == 0
    assert "0.1.0" in r.stdout


# --- Error cases ---


def test_cli_missing_agents_dir_with_toml(tmp_path):
    """When watchd.toml exists but agents_dir doesn't, show helpful error."""
    (tmp_path / "watchd.toml").write_text('[watchd]\nagents_dir = "missing_dir"\n')
    r = _run_cli(tmp_path, "list")
    assert r.returncode != 0
    assert "not found" in r.stderr
