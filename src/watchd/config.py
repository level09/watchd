"""Config loading from watchd.toml."""

from __future__ import annotations

import sys
import tomllib
from dataclasses import dataclass
from pathlib import Path


@dataclass
class DeployConfig:
    host: str = ""
    path: str = ""
    env_file: str = ".env"
    keep_releases: int = 5


@dataclass
class Config:
    db: str = "./watchd.db"
    agents_dir: str = "watchd_agents"
    log_level: str = "info"
    timezone: str = "UTC"
    deploy: DeployConfig | None = None


def load_config(path: Path | None = None) -> Config:
    """Load config from watchd.toml. Returns defaults if file missing."""
    if path is None:
        path = Path.cwd() / "watchd.toml"
    if not path.exists():
        return Config()
    try:
        with open(path, "rb") as f:
            data = tomllib.load(f)
    except tomllib.TOMLDecodeError as e:
        print(f"Error in {path.name}: {e}", file=sys.stderr)
        sys.exit(1)
    watchd = data.get("watchd", {})
    deploy = None
    if "deploy" in watchd:
        d = watchd["deploy"]
        deploy = DeployConfig(
            host=d.get("host", DeployConfig.host),
            path=d.get("path", DeployConfig.path),
            env_file=d.get("env_file", DeployConfig.env_file),
            keep_releases=d.get("keep_releases", DeployConfig.keep_releases),
        )
    return Config(
        db=watchd.get("db", Config.db),
        agents_dir=watchd.get("agents_dir", Config.agents_dir),
        log_level=watchd.get("log_level", Config.log_level),
        timezone=watchd.get("timezone", Config.timezone),
        deploy=deploy,
    )
