"""Scan Show HN for AI leverage ideas using Agno HackerNews tools."""

from datetime import datetime, timezone
from pathlib import Path

from watchd import agent


@agent(
    every="4h",
    name="showhn",
    model="anthropic:claude-sonnet-4-5-20250929",
    instructions=[
        "You scan Show HN for interesting AI projects.",
        "For each project, extract the hidden mechanism, a mental model, and one AI leverage angle.",
        "Be brutally concise. No fluff.",
    ],
    learning=True,
)
def showhn(ctx):
    """Scan Show HN, xray interesting projects for AI leverage ideas."""
    from agno.agent import Agent
    from agno.models.anthropic import Claude
    from agno.tools.hackernews import HackerNewsTools

    a = Agent(
        model=Claude(id="claude-sonnet-4-5-20250929"),
        tools=[HackerNewsTools()],
        learning=True,
        db=ctx.db,
        instructions=[
            "You scan Show HN for interesting AI projects.",
            "For each project, extract the hidden mechanism, a mental model, and one AI leverage angle.",
            "Be brutally concise. No fluff.",
        ],
    )
    response = a.run(
        "Get the top 10 Show HN stories. For each, extract the mechanism, mental model, and AI leverage opportunity.",
        user_id="showhn",
    )

    # Write result to file
    out_dir = Path("output")
    out_dir.mkdir(exist_ok=True)
    ts = datetime.now(timezone.utc).strftime("%Y-%m-%d_%H%M")
    out_file = out_dir / f"showhn_{ts}.md"
    out_file.write_text(response.content)
    ctx.log.info("wrote_output", file=str(out_file))

    return response.content
