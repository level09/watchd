"""Shared output helpers for CLI. TTY-aware, --json support."""

from __future__ import annotations

import json
from datetime import datetime, timezone

from rich.console import Console
from rich.table import Table
from rich.theme import Theme

_theme = Theme(
    {
        "ok": "green",
        "fail": "red",
        "warn": "yellow",
        "info": "blue",
        "dim": "dim",
    }
)

console = Console(theme=_theme, highlight=False)
err_console = Console(theme=_theme, stderr=True, highlight=False)


STATUS_ICONS = {
    "success": "[ok]✓[/ok]",
    "error": "[fail]✗[/fail]",
    "running": "[info]●[/info]",
}


def status_icon(status: str) -> str:
    return STATUS_ICONS.get(status, status)


def relative_time(dt: datetime | None) -> str:
    if dt is None:
        return "-"
    now = datetime.now(timezone.utc)
    if dt.tzinfo is None:
        dt = dt.replace(tzinfo=timezone.utc)
    delta = now - dt
    secs = int(delta.total_seconds())
    if secs < 0:
        return "just now"
    if secs < 60:
        return f"{secs}s ago"
    mins = secs // 60
    if mins < 60:
        return f"{mins}m ago"
    hours = mins // 60
    if hours < 24:
        return f"{hours}h ago"
    days = hours // 24
    return f"{days}d ago"


def print_json(data):
    console.print(json.dumps(data, indent=2, default=str))


def make_table(*columns: str, title: str | None = None) -> Table:
    t = Table(title=title, show_edge=False, pad_edge=False, box=None)
    for col in columns:
        t.add_column(col)
    return t
