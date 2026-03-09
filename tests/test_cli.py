import json
import subprocess
import sys

import pytest


@pytest.fixture
def discovery_dir(tmp_path):
    """Create a temp dir with watchd.toml + agent files."""
    (tmp_path / "watchd.toml").write_text(
        '[watchd]\ndb = "./test.db"\nagents_dir = "watchd_agents"\n'
    )
    agents = tmp_path / "watchd_agents"
    agents.mkdir()
    (agents / "hello.py").write_text(
        "from watchd import agent\n\n"
        "@agent(every='5s')\n"
        "def hello(ctx):\n"
        "    count = ctx.state.get('count', 0) + 1\n"
        "    ctx.state['count'] = count\n"
        "    return f'count={count}'\n"
    )
    (agents / "checker.py").write_text(
        "from watchd import agent\n\n"
        "@agent(every='1h', name='checker')\n"
        "def check_things(ctx):\n"
        "    return 'checked'\n"
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


# --- Core commands ---


def test_cli_list(discovery_dir):
    r = _run_cli(discovery_dir, "list")
    assert r.returncode == 0
    assert "hello" in r.stdout
    assert "checker" in r.stdout


def test_cli_run(discovery_dir):
    r = _run_cli(discovery_dir, "run", "hello")
    assert r.returncode == 0
    assert "count=1" in r.stdout


def test_cli_run_twice_state_persists(discovery_dir):
    _run_cli(discovery_dir, "run", "hello")
    r = _run_cli(discovery_dir, "run", "hello")
    assert "count=2" in r.stdout


def test_cli_history(discovery_dir):
    _run_cli(discovery_dir, "run", "hello")
    r = _run_cli(discovery_dir, "history", "hello")
    assert r.returncode == 0
    assert "hello" in r.stdout


def test_cli_state(discovery_dir):
    _run_cli(discovery_dir, "run", "hello")
    r = _run_cli(discovery_dir, "state", "hello")
    assert r.returncode == 0
    assert '"count": 1' in r.stdout


def test_cli_logs(discovery_dir):
    _run_cli(discovery_dir, "run", "hello")
    r = _run_cli(discovery_dir, "logs", "hello")
    assert r.returncode == 0
    assert "count=1" in r.stdout


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


def test_cli_new_rejects_path_traversal(tmp_path):
    _run_cli(tmp_path, "init")
    r = _run_cli(tmp_path, "new", "../outside")
    assert r.returncode != 0
    assert "Invalid" in r.stderr


def test_cli_new_rejects_invalid_name(tmp_path):
    _run_cli(tmp_path, "init")
    r = _run_cli(tmp_path, "new", "foo bar")
    assert r.returncode != 0
    assert "Invalid" in r.stderr


def test_cli_new_rejects_dashes(tmp_path):
    _run_cli(tmp_path, "init")
    r = _run_cli(tmp_path, "new", "my-agent")
    assert r.returncode != 0
    assert "Invalid" in r.stderr


def test_cli_no_agents_found(tmp_path):
    """When no agents dir and no toml, show helpful error."""
    r = _run_cli(tmp_path, "list")
    assert r.returncode != 0


# --- Status command ---


def test_cli_status(discovery_dir):
    _run_cli(discovery_dir, "run", "hello")
    r = _run_cli(discovery_dir, "status")
    assert r.returncode == 0
    assert "hello" in r.stdout
    assert "checker" in r.stdout


def test_cli_status_single_agent(discovery_dir):
    _run_cli(discovery_dir, "run", "hello")
    r = _run_cli(discovery_dir, "status", "hello")
    assert r.returncode == 0
    assert "hello" in r.stdout
    assert "checker" not in r.stdout


# --- JSON output ---


def test_cli_list_json(discovery_dir):
    r = _run_cli(discovery_dir, "list", "--as-json")
    assert r.returncode == 0
    data = json.loads(r.stdout)
    names = [a["name"] for a in data]
    assert "hello" in names


def test_cli_history_json(discovery_dir):
    _run_cli(discovery_dir, "run", "hello")
    r = _run_cli(discovery_dir, "history", "--as-json")
    assert r.returncode == 0
    data = json.loads(r.stdout)
    assert len(data) >= 1
    assert data[0]["agent"] == "hello"


def test_cli_status_json(discovery_dir):
    _run_cli(discovery_dir, "run", "hello")
    r = _run_cli(discovery_dir, "status", "--as-json")
    assert r.returncode == 0
    data = json.loads(r.stdout)
    names = [a["name"] for a in data]
    assert "hello" in names


# --- Spelling suggestions ---


def test_cli_run_unknown_agent_suggests(discovery_dir):
    r = _run_cli(discovery_dir, "run", "helo")
    assert r.returncode != 0
    assert "Did you mean" in r.stderr
