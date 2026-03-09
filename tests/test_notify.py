"""Tests for notification channels."""

from unittest.mock import MagicMock, patch

import pytest

from watchd.notify import send


def test_log_channel(capsys):
    send("hello world")
    assert "[notify] hello world" in capsys.readouterr().out


def test_log_channel_explicit(capsys):
    send("test msg", channel="log")
    assert "[notify] test msg" in capsys.readouterr().out


def test_slack_channel():
    with patch("httpx.post") as mock_post:
        send("alert!", channel="slack", url="https://hooks.slack.com/test")
    mock_post.assert_called_once_with(
        "https://hooks.slack.com/test",
        json={"text": "alert!"},
        timeout=10,
    )


def test_slack_channel_from_env(monkeypatch):
    monkeypatch.setenv("WATCHD_SLACK_WEBHOOK", "https://hooks.slack.com/env")
    with patch("httpx.post") as mock_post:
        send("from env", channel="slack")
    mock_post.assert_called_once_with(
        "https://hooks.slack.com/env",
        json={"text": "from env"},
        timeout=10,
    )


def test_slack_missing_url():
    with pytest.raises(ValueError, match="Slack webhook URL required"):
        send("fail", channel="slack")


def test_webhook_channel():
    with patch("httpx.post") as mock_post:
        send("event", channel="webhook", url="https://example.com/hook")
    mock_post.assert_called_once_with(
        "https://example.com/hook",
        json={"message": "event"},
        timeout=10,
    )


def test_webhook_custom_payload():
    with patch("httpx.post") as mock_post:
        send("x", channel="webhook", url="https://example.com/hook", payload={"data": "custom"})
    mock_post.assert_called_once_with(
        "https://example.com/hook",
        json={"data": "custom"},
        timeout=10,
    )


def test_webhook_missing_url():
    with pytest.raises(ValueError, match="Webhook URL required"):
        send("fail", channel="webhook")


def test_unknown_channel():
    with pytest.raises(ValueError, match="Unknown notify channel"):
        send("fail", channel="sms")


def test_ctx_notify(capsys):
    """Test notify via AgentContext."""
    import os
    import tempfile

    from watchd.agent import AgentContext
    from watchd.store import Store

    with tempfile.TemporaryDirectory() as tmp:
        store = Store(os.path.join(tmp, "test.db"))
        store.init()
        store.sync_agent("test")
        ctx = AgentContext("test", "run1", store, MagicMock())
        ctx.notify("ctx message")
        assert "[notify] ctx message" in capsys.readouterr().out
