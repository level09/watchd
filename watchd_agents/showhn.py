"""Scan Show HN for AI leverage ideas. Writes analysis to Notion."""

import os
from datetime import datetime, timezone

import httpx
from agno.agent import Agent
from agno.models.anthropic import Claude
from agno.tools.hackernews import HackerNewsTools

NOTION_PAGE_ID = "323f846b-2957-8143-9fc6-f72e02af44d2"

agent = Agent(
    model=Claude(id="claude-sonnet-4-5-20250929"),
    tools=[HackerNewsTools()],
    instructions=[
        "You scan Show HN for interesting projects.",
        "For each project, extract: MECHANISM, MENTAL MODEL, AI LEVERAGE, NON-OBVIOUS PARALLEL.",
        "Be brutally concise. No fluff. Skip boring ones.",
    ],
)

schedule = "4h"
prompt = "Get the top 15 Show HN stories. Skip boring ones. For each interesting one, give the 4-part xray."
