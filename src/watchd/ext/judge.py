"""AI judge pattern. Ask an LLM whether a change is worth acting on."""

from __future__ import annotations

from dataclasses import dataclass
from typing import TYPE_CHECKING

if TYPE_CHECKING:
    from watchd.watch import Change

_DEFAULT_TEMPLATE = """\
A watched resource changed.

Diff:
{diff}

Instruction: {instruction}

Respond in this exact format:
ACT: yes or no
SUMMARY: one sentence explaining why"""


@dataclass
class Verdict:
    should_act: bool
    summary: str
    raw: str


def judge(
    change: Change,
    *,
    instruction: str,
    provider: str = "anthropic",
    template: str | None = None,
    **kwargs,
) -> Verdict:
    """Ask an LLM whether a change is worth acting on."""
    tmpl = template or _DEFAULT_TEMPLATE
    prompt = tmpl.format(diff=change.diff[:4000], instruction=instruction)

    if provider == "anthropic":
        from watchd.ext.anthropic import call
    elif provider == "openai":
        from watchd.ext.openai import call
    else:
        raise ValueError(f"Unknown provider: {provider!r}")

    raw = call(prompt=prompt, **kwargs)
    should_act = "act: yes" in raw.lower()

    summary = ""
    for line in raw.splitlines():
        if line.upper().startswith("SUMMARY:"):
            summary = line.split(":", 1)[1].strip()
            break

    return Verdict(should_act=should_act, summary=summary or raw[:200], raw=raw)
