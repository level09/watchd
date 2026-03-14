"""Watch Show HN for AI leverage ideas. Xray each project's mechanism."""

import httpx

from watchd import agent
from watchd.ext.anthropic import call

HN_API = "https://hacker-news.firebaseio.com/v0"
TOP_N = 15

XRAY_PROMPT = """\
You are an idea xray machine. For each Show HN project below, extract:

1. MECHANISM: The hidden engine that makes it work (not features, the underlying principle)
2. MENTAL MODEL: A one-line analogy or framework for thinking about it
3. AI LEVERAGE: One concrete way this mechanism could be amplified with AI/LLMs

Projects:
{projects}

Be brutally concise. No fluff. Format:

### [title]
- Mechanism: ...
- Mental model: ...
- AI leverage: ..."""


@agent(every="4h", name="showhn")
def showhn(ctx):
    """Scan Show HN, xray interesting projects for AI leverage ideas."""
    seen = set(ctx.state.get("seen_ids", []))

    story_ids = httpx.get(f"{HN_API}/showstories.json", timeout=15).json()[:30]

    new_stories = []
    for sid in story_ids:
        if str(sid) in seen:
            continue
        item = httpx.get(f"{HN_API}/item/{sid}.json", timeout=10).json()
        if not item or not item.get("title"):
            continue
        new_stories.append(item)
        seen.add(str(sid))
        if len(new_stories) >= TOP_N:
            break

    ctx.state["seen_ids"] = list(seen)[-200:]

    if not new_stories:
        return "no new stories"

    projects = "\n".join(
        f"- {s['title']} ({s.get('url', 'no url')}) [{s.get('score', 0)} pts]"
        for s in new_stories
    )

    analysis = call(
        prompt=XRAY_PROMPT.format(projects=projects),
        model="claude-sonnet-4-20250514",
        max_tokens=2048,
    )

    ctx.log.info("xrayed", count=len(new_stories))
    print(analysis)
    return f"xrayed {len(new_stories)} projects"
