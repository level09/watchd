"""Notification channels for watchd agents."""

from __future__ import annotations

import os


def send(message: str, *, channel: str = "log", **kwargs):
    """Send a notification via the specified channel.

    Channels: "log" (stdout), "slack" (webhook), "webhook" (arbitrary URL).
    """
    if channel == "log":
        print(f"[notify] {message}")
    elif channel == "slack":
        _slack(message, kwargs)
    elif channel == "webhook":
        _webhook(message, kwargs)
    else:
        raise ValueError(f"Unknown notify channel: {channel!r}")


def _slack(message: str, config: dict):
    import httpx

    url = config.get("url") or os.environ.get("WATCHD_SLACK_WEBHOOK")
    if not url:
        raise ValueError("Slack webhook URL required (url= kwarg or WATCHD_SLACK_WEBHOOK env)")
    httpx.post(url, json={"text": message}, timeout=10)


def _webhook(message: str, config: dict):
    import httpx

    url = config.get("url")
    if not url:
        raise ValueError("Webhook URL required (url= kwarg)")
    payload = config.get("payload", {"message": message})
    httpx.post(url, json=payload, timeout=10)
