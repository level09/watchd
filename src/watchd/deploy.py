"""SSH + systemd atomic deploys for watchd agents."""

from __future__ import annotations

import subprocess
import sys
import time
from datetime import datetime, timezone
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

_RSYNC_EXCLUDES = (
    ".venv",
    "__pycache__",
    "*.db",
    ".git",
    ".env",
    ".ruff_cache",
    "*.pyc",
)


def _ssh(host, cmd, check=True):
    result = subprocess.run(
        ["ssh", "-o", "BatchMode=yes", "-o", "ConnectTimeout=10", host, cmd],
        capture_output=True,
        text=True,
    )
    if check and result.returncode != 0:
        raise RuntimeError(f"ssh command failed: {cmd}\n{result.stderr.strip()}")
    return result


def _rsync(source, host, dest):
    args = ["rsync", "-az", "--delete"]
    for exc in _RSYNC_EXCLUDES:
        args += ["--exclude", exc]
    source_str = str(source).rstrip("/") + "/"
    args += [source_str, f"{host}:{dest}/"]
    result = subprocess.run(args, capture_output=True, text=True)
    if result.returncode != 0:
        raise RuntimeError(f"rsync failed:\n{result.stderr.strip()}")
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
            keep_releases=dc.keep_releases,
        )
    return dc


def _validate_local(config):
    errors = []
    if not (Path.cwd() / "watchd.toml").exists():
        errors.append("watchd.toml not found")
    if not (Path.cwd() / "pyproject.toml").exists():
        errors.append("pyproject.toml not found")
    agents_dir = Path.cwd() / config.agents_dir
    if not agents_dir.is_dir():
        errors.append(f"agents directory '{config.agents_dir}' not found")
    if errors:
        for e in errors:
            print(f"  [FAIL] {e}", file=sys.stderr)
        sys.exit(1)


def preflight(config):
    dc = _resolve_deploy_config(config)
    all_pass = True

    def _check(label, fn):
        nonlocal all_pass
        try:
            fn()
            print(f"  [PASS] {label}")
        except Exception as e:
            print(f"  [FAIL] {label}: {e}")
            all_pass = False
            return False
        return True

    print("Preflight checks:")
    if not _check("SSH connectivity", lambda: _ssh(dc.host, "echo ok")):
        return False

    _check("uv available", lambda: _ssh(dc.host, "command -v uv"))
    _check("systemctl available", lambda: _ssh(dc.host, "command -v systemctl"))

    def _check_linger():
        r = _ssh(dc.host, "loginctl show-user $(whoami) -p Linger 2>/dev/null || echo Linger=unknown", check=False)
        out = r.stdout.strip()
        if "Linger=no" in out:
            raise RuntimeError("loginctl linger not enabled, service will stop on logout. Run: loginctl enable-linger")

    _check("loginctl linger", _check_linger)
    _check("deploy path writable", lambda: _ssh(dc.host, f"mkdir -p {dc.path} && test -w {dc.path}"))

    return all_pass


def deploy(config):
    _validate_local(config)
    dc = _resolve_deploy_config(config)

    print("Running preflight checks...")
    if not preflight(config):
        print("\nPreflight failed. Fix issues above and retry.", file=sys.stderr)
        sys.exit(1)

    ts = datetime.now(timezone.utc).strftime("%Y%m%d-%H%M%S")
    release_dir = f"{dc.path}/releases/{ts}"
    base = dc.path

    print(f"\nDeploying to {dc.host}:{release_dir}")

    # Create dirs
    _ssh(dc.host, f"mkdir -p {release_dir} && mkdir -p {base}/shared")

    # Rsync project files
    print("  Syncing files...")
    _rsync(Path.cwd(), dc.host, release_dir)

    # Transfer .env if exists
    env_path = Path.cwd() / dc.env_file
    if env_path.exists():
        print("  Transferring .env...")
        subprocess.run(
            ["rsync", "-az", str(env_path), f"{dc.host}:{release_dir}/.env"],
            capture_output=True,
            text=True,
            check=True,
        )

    # uv sync before symlink swap (failure keeps old release live)
    print("  Installing dependencies...")
    _ssh(dc.host, f"cd {release_dir} && uv sync")

    # Symlink shared db (use configured db path, default watchd.db)
    db_name = Path(config.db).name
    _ssh(dc.host, f"ln -sfn ../../shared/{db_name} {release_dir}/{db_name}")

    # Atomic symlink swap
    print("  Swapping symlink...")
    _ssh(dc.host, f"ln -sfn releases/{ts} {base}/current.tmp && mv -Tf {base}/current.tmp {base}/current")

    # Resolve paths for systemd unit
    abs_path = _ssh(dc.host, f"realpath {base}/current").stdout.strip()
    uv_path = _ssh(dc.host, "command -v uv").stdout.strip()

    # Derive service name from remote path basename
    service_base = Path(dc.path.replace("~", "")).name if "~" in dc.path else Path(dc.path).name
    service_name = f"watchd-{service_base}"

    # Generate and write systemd unit
    unit = _generate_unit(service_name, abs_path, uv_path)
    unit_path = f"~/.config/systemd/user/{service_name}.service"
    _ssh(dc.host, "mkdir -p ~/.config/systemd/user")
    _ssh(dc.host, f"cat > {unit_path} << 'UNIT_EOF'\n{unit}UNIT_EOF")

    # Reload, enable, restart
    print("  Starting service...")
    _ssh(dc.host, f"systemctl --user daemon-reload && systemctl --user enable {service_name} && systemctl --user restart {service_name}")

    # Status check
    time.sleep(2)
    status = _ssh(dc.host, f"systemctl --user is-active {service_name}", check=False)
    if status.stdout.strip() == "active":
        print(f"\n  {service_name} is running.")
    else:
        print(f"\n  Warning: {service_name} status: {status.stdout.strip()}", file=sys.stderr)
        detail = _ssh(dc.host, f"systemctl --user status {service_name}", check=False)
        print(detail.stdout, file=sys.stderr)

    # Prune old releases
    _prune_releases(dc.host, base, dc.keep_releases)

    print("Deploy complete.")


def _prune_releases(host, base, keep):
    result = _ssh(host, f"ls -1t {base}/releases/", check=False)
    if result.returncode != 0:
        return
    dirs = [d for d in result.stdout.strip().split("\n") if d]
    if len(dirs) <= keep:
        return
    to_remove = dirs[keep:]
    for d in to_remove:
        _ssh(host, f"rm -rf {base}/releases/{d}", check=False)
    print(f"  Pruned {len(to_remove)} old release(s).")
