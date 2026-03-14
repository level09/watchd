"""Scan Show HN for AI leverage ideas using Agno HackerNews tools."""

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
def showhn():
    """Scan Show HN, xray interesting projects for AI leverage ideas."""
    return "Get the top 10 Show HN stories. For each, extract the mechanism, mental model, and AI leverage opportunity."
