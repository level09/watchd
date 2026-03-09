"""Deploy and systemd service management for watchd agents."""

from __future__ import annotations

import shutil
import subprocess
import sys
import time
from pathlib import Path

from watchd.config import DeployConfig

_UNIT_TEMPLATE = """\
[Unit]
Description=watchd: {name}
After=network.target

[Service]
Type=simple
WorkingDirectory={project_path}
ExecStart={uv_path} run watchd up
Restart=on-failure
RestartSec=10
EnvironmentFile=-{project_path}/.env

[Install]
WantedBy=default.target
"""


def _ssh(host, cmd, check=True):
    result = subprocess.run(
        ["ssh", "-o", "BatchMode=yes", "-o", "ConnectTimeout=10", host, cmd],
        capture_output=True,
        text=True,
    )
    if check and result.returncode != 0:
        raise RuntimeError(f"ssh command failed: {cmd}\n{result.stderr.strip()}")
    return result


def _generate_unit(name, project_path, uv_path):
    return _UNIT_TEMPLATE.format(name=name, project_path=project_path, uv_path=uv_path)


def _resolve_deploy_config(config):
    if config.deploy is None:
        print("No [watchd.deploy] section in watchd.toml.", file=sys.stderr)
        sys.exit(1)
    dc = config.deploy
    if not dc.host:
        print("deploy.host is required in watchd.toml.", file=sys.stderr)
        sys.exit(1)
    if not dc.path:
        dc = DeployConfig(
            host=dc.host,
            path=f"~/watchd-{Path.cwd().name}",
            env_file=dc.env_file,
        )
    return dc


def _get_git_remote():
    result = subprocess.run(
        ["git", "remote", "get-url", "origin"],
        capture_output=True,
        text=True,
    )
    if result.returncode != 0:
        print("No git remote 'origin' found.", file=sys.stderr)
        sys.exit(1)
    return result.stdout.strip()


def setup(config):
    """First-time server setup: clone repo, install deps, create systemd service."""
    dc = _resolve_deploy_config(config)
    repo = _get_git_remote()

    print(f"Setting up {dc.host}:{dc.path}")

    # Check SSH + uv
    _ssh(dc.host, "echo ok")
    _ssh(dc.host, "command -v uv")

    # Clone
    _ssh(dc.host, f"git clone {repo} {dc.path}")

    # Install deps
    print("  Installing dependencies...")
    _ssh(dc.host, f"cd {dc.path} && uv sync")

    # Transfer .env
    env_path = Path.cwd() / dc.env_file
    if env_path.exists():
        print("  Transferring .env...")
        subprocess.run(
            ["scp", str(env_path), f"{dc.host}:{dc.path}/.env"],
            capture_output=True,
            text=True,
            check=True,
        )

    # Install systemd service on remote
    print("  Installing service...")
    _ssh(dc.host, f"cd {dc.path} && uv run watchd install")

    print("Done. Run 'watchd deploy' for future updates.")


def deploy(config):
    """Pull latest code, sync deps, restart service."""
    dc = _resolve_deploy_config(config)
    service_name = f"watchd-{Path(dc.path).name}"

    print(f"Deploying to {dc.host}:{dc.path}")

    # Pull latest
    print("  Pulling latest...")
    _ssh(dc.host, f"cd {dc.path} && git pull")

    # Sync deps
    print("  Syncing dependencies...")
    _ssh(dc.host, f"cd {dc.path} && uv sync")

    # Transfer .env if exists locally
    env_path = Path.cwd() / dc.env_file
    if env_path.exists():
        print("  Transferring .env...")
        subprocess.run(
            ["scp", str(env_path), f"{dc.host}:{dc.path}/.env"],
            capture_output=True,
            text=True,
            check=True,
        )

    # Restart service
    print("  Restarting service...")
    _ssh(dc.host, f"systemctl --user restart {service_name}")

    # Verify
    time.sleep(2)
    status = _ssh(dc.host, f"systemctl --user is-active {service_name}", check=False)
    if status.stdout.strip() == "active":
        print(f"  {service_name} is running.")
    else:
        print(f"  Warning: {service_name} status: {status.stdout.strip()}", file=sys.stderr)

    print("Done.")


def install(config):
    """Install watchd as a systemd user service on the local machine."""
    service_name = f"watchd-{Path.cwd().name}"
    uv_path = shutil.which("uv")
    if not uv_path:
        print("uv not found in PATH.", file=sys.stderr)
        sys.exit(1)

    project_path = str(Path.cwd())
    unit = _generate_unit(service_name, project_path, uv_path)

    unit_dir = Path.home() / ".config" / "systemd" / "user"
    unit_dir.mkdir(parents=True, exist_ok=True)
    unit_file = unit_dir / f"{service_name}.service"
    unit_file.write_text(unit)
    print(f"Wrote {unit_file}")

    subprocess.run(["systemctl", "--user", "daemon-reload"], check=True)
    subprocess.run(["systemctl", "--user", "enable", service_name], check=True)
    subprocess.run(["systemctl", "--user", "start", service_name], check=True)

    time.sleep(2)
    status = subprocess.run(
        ["systemctl", "--user", "is-active", service_name],
        capture_output=True,
        text=True,
    )
    if status.stdout.strip() == "active":
        print(f"{service_name} is running.")
    else:
        print(f"Warning: {service_name} status: {status.stdout.strip()}", file=sys.stderr)


def uninstall(config):
    """Remove the watchd systemd user service."""
    service_name = f"watchd-{Path.cwd().name}"

    subprocess.run(["systemctl", "--user", "stop", service_name], capture_output=True)
    subprocess.run(["systemctl", "--user", "disable", service_name], capture_output=True)

    unit_file = Path.home() / ".config" / "systemd" / "user" / f"{service_name}.service"
    if unit_file.exists():
        unit_file.unlink()
        print(f"Removed {unit_file}")

    subprocess.run(["systemctl", "--user", "daemon-reload"], capture_output=True)
    print(f"{service_name} uninstalled.")
