import subprocess
from pathlib import Path
from unittest.mock import MagicMock, patch

import pytest

from watchd.config import Config, DeployConfig
from watchd.deploy import (
    _generate_unit,
    _prune_releases,
    _resolve_deploy_config,
    _validate_local,
    deploy,
    preflight,
)


def _ok(stdout="", stderr=""):
    r = MagicMock(spec=subprocess.CompletedProcess)
    r.returncode = 0
    r.stdout = stdout
    r.stderr = stderr
    return r


def _fail(stderr="error"):
    r = MagicMock(spec=subprocess.CompletedProcess)
    r.returncode = 1
    r.stdout = ""
    r.stderr = stderr
    return r


# --- resolve_deploy_config ---


def test_resolve_deploy_config_defaults():
    dc = DeployConfig(host="u@host")
    config = Config(deploy=dc)
    resolved = _resolve_deploy_config(config)
    assert resolved.host == "u@host"
    assert resolved.path == f"~/watchd-{Path.cwd().name}"
    assert resolved.env_file == ".env"
    assert resolved.keep_releases == 5


def test_resolve_deploy_config_with_path():
    dc = DeployConfig(host="u@host", path="~/custom-path")
    config = Config(deploy=dc)
    resolved = _resolve_deploy_config(config)
    assert resolved.path == "~/custom-path"


def test_resolve_deploy_config_missing_host():
    dc = DeployConfig()
    config = Config(deploy=dc)
    with pytest.raises(SystemExit):
        _resolve_deploy_config(config)


def test_resolve_deploy_config_no_section():
    config = Config()
    with pytest.raises(SystemExit):
        _resolve_deploy_config(config)


# --- generate_unit ---


def test_generate_unit():
    unit = _generate_unit("watchd-myproject", "/home/user/watchd-myproject/releases/123", "/home/user/.local/bin/uv")
    assert "Description=watchd: watchd-myproject" in unit
    assert "WorkingDirectory=/home/user/watchd-myproject/releases/123" in unit
    assert "ExecStart=/home/user/.local/bin/uv run watchd up" in unit
    assert "Restart=on-failure" in unit
    assert "WantedBy=default.target" in unit


# --- validate_local ---


def test_validate_local_all_present(tmp_path, monkeypatch):
    monkeypatch.chdir(tmp_path)
    (tmp_path / "watchd.toml").write_text("[watchd]\n")
    (tmp_path / "pyproject.toml").write_text("[project]\n")
    (tmp_path / "watchd_agents").mkdir()
    _validate_local(Config())


def test_validate_local_missing_toml(tmp_path, monkeypatch):
    monkeypatch.chdir(tmp_path)
    (tmp_path / "pyproject.toml").write_text("[project]\n")
    (tmp_path / "watchd_agents").mkdir()
    with pytest.raises(SystemExit):
        _validate_local(Config())


def test_validate_local_missing_pyproject(tmp_path, monkeypatch):
    monkeypatch.chdir(tmp_path)
    (tmp_path / "watchd.toml").write_text("[watchd]\n")
    (tmp_path / "watchd_agents").mkdir()
    with pytest.raises(SystemExit):
        _validate_local(Config())


def test_validate_local_missing_agents_dir(tmp_path, monkeypatch):
    monkeypatch.chdir(tmp_path)
    (tmp_path / "watchd.toml").write_text("[watchd]\n")
    (tmp_path / "pyproject.toml").write_text("[project]\n")
    with pytest.raises(SystemExit):
        _validate_local(Config())


# --- preflight ---


@patch("watchd.deploy.subprocess.run")
def test_preflight_all_pass(mock_run):
    mock_run.return_value = _ok("ok\n")
    dc = DeployConfig(host="u@host", path="~/myapp")
    config = Config(deploy=dc)
    assert preflight(config) is True


@patch("watchd.deploy.subprocess.run")
def test_preflight_ssh_fails(mock_run):
    mock_run.return_value = _fail("Connection refused")
    dc = DeployConfig(host="u@host", path="~/myapp")
    config = Config(deploy=dc)
    assert preflight(config) is False


@patch("watchd.deploy.subprocess.run")
def test_preflight_linger_warning(mock_run):
    def side_effect(args, **kwargs):
        cmd = args[-1] if args else ""
        if "loginctl" in cmd:
            return _ok("Linger=no")
        return _ok("ok\n")

    mock_run.side_effect = side_effect
    dc = DeployConfig(host="u@host", path="~/myapp")
    config = Config(deploy=dc)
    assert preflight(config) is False


# --- deploy flow ---


@patch("watchd.deploy.time.sleep")
@patch("watchd.deploy.subprocess.run")
def test_deploy_flow_sequence(mock_run, mock_sleep, tmp_path, monkeypatch):
    monkeypatch.chdir(tmp_path)
    (tmp_path / "watchd.toml").write_text("[watchd]\n")
    (tmp_path / "pyproject.toml").write_text("[project]\n")
    (tmp_path / "watchd_agents").mkdir()

    calls, track_run = _make_tracker()
    mock_run.side_effect = track_run
    dc = DeployConfig(host="u@host", path="~/myapp")
    config = Config(deploy=dc)
    deploy(config)

    # uv sync happens before symlink swap
    uv_sync_idx = next(i for i, c in enumerate(calls) if "uv sync" in c)
    symlink_idx = next(i for i, c in enumerate(calls) if "current.tmp" in c and "mv" in c)
    assert uv_sync_idx < symlink_idx

    # rsync was called
    assert any("rsync" in c for c in calls)

    # systemctl restart happens after symlink
    restart_idx = next(i for i, c in enumerate(calls) if "restart" in c)
    assert restart_idx > symlink_idx


def _make_tracker():
    calls = []

    def track_run(args, **kwargs):
        cmd = " ".join(str(a) for a in args)
        calls.append(cmd)
        if "realpath" in cmd:
            return _ok("/home/user/myapp/releases/123\n")
        if "command -v uv" in cmd:
            return _ok("/home/user/.local/bin/uv\n")
        if "is-active" in cmd:
            return _ok("active\n")
        if "ls -1t" in cmd:
            return _ok("20260222-150102\n")
        if "&& pwd" in cmd:
            return _ok("/home/user/myapp/shared\n")
        return _ok()

    return calls, track_run


@patch("watchd.deploy.time.sleep")
@patch("watchd.deploy.subprocess.run")
def test_env_file_transferred_when_exists(mock_run, mock_sleep, tmp_path, monkeypatch):
    monkeypatch.chdir(tmp_path)
    (tmp_path / "watchd.toml").write_text("[watchd]\n")
    (tmp_path / "pyproject.toml").write_text("[project]\n")
    (tmp_path / "watchd_agents").mkdir()
    (tmp_path / ".env").write_text("SECRET=abc\n")

    calls, track_run = _make_tracker()
    mock_run.side_effect = track_run
    dc = DeployConfig(host="u@host", path="~/myapp")
    config = Config(deploy=dc)
    deploy(config)

    env_transfer = [c for c in calls if "rsync" in c and c.endswith(".env")]
    assert len(env_transfer) >= 1


@patch("watchd.deploy.time.sleep")
@patch("watchd.deploy.subprocess.run")
def test_env_file_not_transferred_when_missing(mock_run, mock_sleep, tmp_path, monkeypatch):
    monkeypatch.chdir(tmp_path)
    (tmp_path / "watchd.toml").write_text("[watchd]\n")
    (tmp_path / "pyproject.toml").write_text("[project]\n")
    (tmp_path / "watchd_agents").mkdir()

    calls, track_run = _make_tracker()
    mock_run.side_effect = track_run
    dc = DeployConfig(host="u@host", path="~/myapp")
    config = Config(deploy=dc)
    deploy(config)

    env_transfer = [c for c in calls if "rsync" in c and c.endswith(".env")]
    assert len(env_transfer) == 0


@patch("watchd.deploy.time.sleep")
@patch("watchd.deploy.subprocess.run")
def test_deploy_db_subdir_path(mock_run, mock_sleep, tmp_path, monkeypatch):
    """db = './data/watchd.db' should mkdir data/ and symlink correctly."""
    monkeypatch.chdir(tmp_path)
    (tmp_path / "watchd.toml").write_text("[watchd]\n")
    (tmp_path / "pyproject.toml").write_text("[project]\n")
    (tmp_path / "watchd_agents").mkdir()

    calls, track_run = _make_tracker()
    mock_run.side_effect = track_run
    dc = DeployConfig(host="u@host", path="~/myapp")
    config = Config(db="./data/watchd.db", deploy=dc)
    deploy(config)

    # Should mkdir the data/ subdir inside the release
    mkdir_calls = [c for c in calls if "mkdir -p" in c and "/data" in c]
    assert len(mkdir_calls) >= 1

    # Symlink target should be the absolute shared path, link at data/watchd.db
    ln_calls = [c for c in calls if "ln -sfn" in c and "shared" in c and "data/watchd.db" in c]
    assert len(ln_calls) == 1


@patch("watchd.deploy.time.sleep")
@patch("watchd.deploy.subprocess.run")
def test_deploy_rejects_absolute_db_path(mock_run, mock_sleep, tmp_path, monkeypatch):
    monkeypatch.chdir(tmp_path)
    (tmp_path / "watchd.toml").write_text("[watchd]\n")
    (tmp_path / "pyproject.toml").write_text("[project]\n")
    (tmp_path / "watchd_agents").mkdir()

    calls, track_run = _make_tracker()
    mock_run.side_effect = track_run
    dc = DeployConfig(host="u@host", path="~/myapp")
    config = Config(db="/absolute/path/watchd.db", deploy=dc)
    with pytest.raises(SystemExit):
        deploy(config)


@patch("watchd.deploy.time.sleep")
@patch("watchd.deploy.subprocess.run")
def test_deploy_rejects_dotdot_db_path(mock_run, mock_sleep, tmp_path, monkeypatch):
    monkeypatch.chdir(tmp_path)
    (tmp_path / "watchd.toml").write_text("[watchd]\n")
    (tmp_path / "pyproject.toml").write_text("[project]\n")
    (tmp_path / "watchd_agents").mkdir()

    calls, track_run = _make_tracker()
    mock_run.side_effect = track_run
    dc = DeployConfig(host="u@host", path="~/myapp")
    config = Config(db="../escape/watchd.db", deploy=dc)
    with pytest.raises(SystemExit):
        deploy(config)


# --- prune_releases ---


@patch("watchd.deploy.subprocess.run")
def test_prune_releases(mock_run):
    ls_result = _ok("20260222-150102\n20260222-143052\n20260221-120000\n20260220-100000\n")
    rm_results = [_ok(), _ok()]

    results = [ls_result] + rm_results
    mock_run.side_effect = results

    _prune_releases("u@host", "~/myapp", 2)

    # ls + 2 rm calls
    assert mock_run.call_count == 3


@patch("watchd.deploy.subprocess.run")
def test_prune_releases_nothing_to_prune(mock_run):
    mock_run.return_value = _ok("20260222-150102\n20260222-143052\n")
    _prune_releases("u@host", "~/myapp", 5)
    assert mock_run.call_count == 1  # only ls
