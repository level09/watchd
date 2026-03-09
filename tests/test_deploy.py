"""Tests for deploy and systemd service management."""

import subprocess
from pathlib import Path
from unittest.mock import MagicMock, patch

import pytest

from watchd.config import Config, DeployConfig
from watchd.deploy import (
    _generate_unit,
    _resolve_deploy_config,
    deploy,
    install,
    setup,
    uninstall,
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
    unit = _generate_unit("watchd-myproject", "/home/user/myproject", "/home/user/.local/bin/uv")
    assert "Description=watchd: watchd-myproject" in unit
    assert "WorkingDirectory=/home/user/myproject" in unit
    assert "ExecStart=/home/user/.local/bin/uv run watchd up" in unit
    assert "Restart=on-failure" in unit
    assert "WantedBy=default.target" in unit


# --- deploy ---


@patch("watchd.deploy.time.sleep")
@patch("watchd.deploy.subprocess.run")
@patch("watchd.deploy._ssh")
def test_deploy_flow(mock_ssh, mock_run, mock_sleep, tmp_path, monkeypatch):
    monkeypatch.chdir(tmp_path)
    (tmp_path / ".env").write_text("SECRET=abc\n")

    active_result = MagicMock()
    active_result.stdout = "active\n"
    mock_ssh.side_effect = [
        None,  # git pull
        None,  # uv sync
        None,  # restart
        active_result,  # is-active
    ]

    dc = DeployConfig(host="u@host", path="~/myapp")
    config = Config(deploy=dc)
    deploy(config)

    calls = [c.args for c in mock_ssh.call_args_list]
    assert "git pull" in calls[0][1]
    assert "uv sync" in calls[1][1]
    assert "restart" in calls[2][1]


@patch("watchd.deploy.time.sleep")
@patch("watchd.deploy.subprocess.run")
@patch("watchd.deploy._ssh")
def test_deploy_env_transferred(mock_ssh, mock_run, mock_sleep, tmp_path, monkeypatch):
    monkeypatch.chdir(tmp_path)
    (tmp_path / ".env").write_text("SECRET=abc\n")

    mock_ssh.return_value = MagicMock(stdout="active\n")
    dc = DeployConfig(host="u@host", path="~/myapp")
    deploy(Config(deploy=dc))

    scp_calls = [c for c in mock_run.call_args_list if "scp" in str(c)]
    assert len(scp_calls) == 1


@patch("watchd.deploy.time.sleep")
@patch("watchd.deploy.subprocess.run")
@patch("watchd.deploy._ssh")
def test_deploy_no_env_file(mock_ssh, mock_run, mock_sleep, tmp_path, monkeypatch):
    monkeypatch.chdir(tmp_path)

    mock_ssh.return_value = MagicMock(stdout="active\n")
    dc = DeployConfig(host="u@host", path="~/myapp")
    deploy(Config(deploy=dc))

    scp_calls = [c for c in mock_run.call_args_list if "scp" in str(c)]
    assert len(scp_calls) == 0


# --- setup ---


@patch("watchd.deploy.subprocess.run")
@patch("watchd.deploy._ssh")
@patch("watchd.deploy._get_git_remote", return_value="git@github.com:user/repo.git")
def test_setup_flow(mock_remote, mock_ssh, mock_run, tmp_path, monkeypatch):
    monkeypatch.chdir(tmp_path)
    mock_ssh.return_value = MagicMock(stdout="ok\n")

    dc = DeployConfig(host="u@host", path="~/myapp")
    setup(Config(deploy=dc))

    calls = [c.args for c in mock_ssh.call_args_list]
    assert any("echo ok" in c[1] for c in calls)
    assert any("git clone" in c[1] for c in calls)
    assert any("uv sync" in c[1] for c in calls)
    assert any("watchd install" in c[1] for c in calls)


@patch("watchd.deploy.subprocess.run")
@patch("watchd.deploy._ssh")
@patch("watchd.deploy._get_git_remote", return_value="git@github.com:user/repo.git")
def test_setup_transfers_env(mock_remote, mock_ssh, mock_run, tmp_path, monkeypatch):
    monkeypatch.chdir(tmp_path)
    (tmp_path / ".env").write_text("KEY=val\n")
    mock_ssh.return_value = MagicMock(stdout="ok\n")

    dc = DeployConfig(host="u@host", path="~/myapp")
    setup(Config(deploy=dc))

    scp_calls = [c for c in mock_run.call_args_list if "scp" in str(c)]
    assert len(scp_calls) == 1


# --- install / uninstall ---


@patch("watchd.deploy.time.sleep")
@patch("watchd.deploy.subprocess.run")
@patch("watchd.deploy.shutil.which", return_value="/usr/local/bin/uv")
def test_install_generates_correct_unit(mock_which, mock_run, mock_sleep, tmp_path, monkeypatch):
    monkeypatch.chdir(tmp_path)

    mock_run.return_value = _ok("active\n")
    install(Config())

    unit_file = Path.home() / ".config" / "systemd" / "user" / f"watchd-{tmp_path.name}.service"
    assert unit_file.exists()
    content = unit_file.read_text()
    assert f"WorkingDirectory={tmp_path}" in content
    assert "ExecStart=/usr/local/bin/uv run watchd up" in content
    unit_file.unlink()


@patch("watchd.deploy.subprocess.run")
def test_uninstall_removes_unit_file(mock_run, tmp_path, monkeypatch):
    monkeypatch.chdir(tmp_path)
    service_name = f"watchd-{tmp_path.name}"
    unit_dir = Path.home() / ".config" / "systemd" / "user"
    unit_dir.mkdir(parents=True, exist_ok=True)
    unit_file = unit_dir / f"{service_name}.service"
    unit_file.write_text("[Unit]\n")

    mock_run.return_value = _ok()
    uninstall(Config())

    assert not unit_file.exists()


@patch("watchd.deploy.shutil.which", return_value=None)
def test_install_checks_uv_available(mock_which, tmp_path, monkeypatch):
    monkeypatch.chdir(tmp_path)
    with pytest.raises(SystemExit):
        install(Config())
