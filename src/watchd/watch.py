"""Watch primitives. Detect changes in URLs, files, and command output."""

from __future__ import annotations

import difflib
import subprocess
from dataclasses import dataclass
from pathlib import Path
from typing import TYPE_CHECKING

if TYPE_CHECKING:
    from watchd.agent import AgentContext


@dataclass
class Change:
    before: str
    after: str
    diff: str
    summary: str
    new: str | None = None


def url(target: str, *, ctx: AgentContext, headers: dict | None = None) -> Change | None:
    """Fetch URL, compare to last fetch stored in ctx.state."""
    import httpx

    resp = httpx.get(target, headers=headers or {}, follow_redirects=True, timeout=30)
    resp.raise_for_status()
    content = resp.text
    state_key = f"_watch:url:{target}"
    prev = ctx.state.get(state_key, "")
    ctx.state[state_key] = content
    if not prev or content == prev:
        return None
    return _make_change(prev, content)


def file(target: str, *, ctx: AgentContext, mode: str = "full") -> Change | None:
    """Read file, compare to last read.

    mode="full": detect any change in file content.
    mode="tail": return only new content appended since last check.
    """
    path = Path(target)
    content = path.read_text()
    state_key = f"_watch:file:{target}"

    if mode == "tail":
        prev_len = ctx.state.get(state_key, 0)
        ctx.state[state_key] = len(content)
        if not prev_len or len(content) <= prev_len:
            return None
        new_content = content[prev_len:]
        n_lines = new_content.count("\n")
        return Change(
            before="",
            after=new_content,
            diff=new_content,
            summary=f"{n_lines} new lines",
            new=new_content,
        )

    prev = ctx.state.get(state_key, "")
    ctx.state[state_key] = content
    if not prev or content == prev:
        return None
    return _make_change(prev, content)


def command(cmd: str, *, ctx: AgentContext, timeout: int = 60) -> Change | None:
    """Run shell command, compare output to last run."""
    result = subprocess.run(cmd, shell=True, capture_output=True, text=True, timeout=timeout)
    output = result.stdout
    state_key = f"_watch:cmd:{cmd}"
    prev = ctx.state.get(state_key, "")
    ctx.state[state_key] = output
    if not prev or output == prev:
        return None
    return _make_change(prev, output)


def _make_change(before: str, after: str) -> Change:
    diff_lines = list(
        difflib.unified_diff(
            before.splitlines(keepends=True),
            after.splitlines(keepends=True),
        )
    )
    diff = "".join(diff_lines)
    added = sum(1 for line in diff_lines if line.startswith("+") and not line.startswith("+++"))
    removed = sum(1 for line in diff_lines if line.startswith("-") and not line.startswith("---"))
    summary = f"{added} added, {removed} removed"
    return Change(before=before, after=after, diff=diff, summary=summary)
