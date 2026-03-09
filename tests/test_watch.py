"""Tests for watch primitives (url, file, command)."""

from unittest.mock import MagicMock, patch

import pytest

from watchd.agent import AgentContext
from watchd.store import Store
from watchd.watch import Change, command, file, url


@pytest.fixture
def store(tmp_path):
    s = Store(str(tmp_path / "test.db"))
    s.init()
    s.sync_agent("test_agent")
    return s


@pytest.fixture
def ctx(store):
    return AgentContext("test_agent", "run123", store, MagicMock())


# -- Change dataclass --


def test_change_fields():
    c = Change(before="a", after="b", diff="diff", summary="1 added, 1 removed")
    assert c.before == "a"
    assert c.after == "b"
    assert c.new is None


# -- watch.file --


def test_file_first_run_returns_none(tmp_path, ctx):
    f = tmp_path / "data.txt"
    f.write_text("hello")
    result = file(str(f), ctx=ctx)
    assert result is None


def test_file_no_change(tmp_path, ctx):
    f = tmp_path / "data.txt"
    f.write_text("hello")
    file(str(f), ctx=ctx)
    result = file(str(f), ctx=ctx)
    assert result is None


def test_file_detects_change(tmp_path, ctx):
    f = tmp_path / "data.txt"
    f.write_text("hello")
    file(str(f), ctx=ctx)
    f.write_text("hello\nworld")
    result = file(str(f), ctx=ctx)
    assert result is not None
    assert isinstance(result, Change)
    assert "world" in result.after
    assert "added" in result.summary


def test_file_tail_mode_first_run(tmp_path, ctx):
    f = tmp_path / "log.txt"
    f.write_text("line1\n")
    result = file(str(f), ctx=ctx, mode="tail")
    assert result is None


def test_file_tail_mode_detects_new_lines(tmp_path, ctx):
    f = tmp_path / "log.txt"
    f.write_text("line1\n")
    file(str(f), ctx=ctx, mode="tail")
    f.write_text("line1\nline2\nline3\n")
    result = file(str(f), ctx=ctx, mode="tail")
    assert result is not None
    assert "line2" in result.new
    assert "line3" in result.new
    assert "line1" not in result.new
    assert "2 new lines" in result.summary


def test_file_tail_mode_no_new_lines(tmp_path, ctx):
    f = tmp_path / "log.txt"
    f.write_text("line1\n")
    file(str(f), ctx=ctx, mode="tail")
    result = file(str(f), ctx=ctx, mode="tail")
    assert result is None


def test_file_tail_mode_truncated(tmp_path, ctx):
    """If file gets shorter (truncated/rotated), reset and return None."""
    f = tmp_path / "log.txt"
    f.write_text("long content here\n")
    file(str(f), ctx=ctx, mode="tail")
    f.write_text("short\n")
    result = file(str(f), ctx=ctx, mode="tail")
    assert result is None


# -- watch.command --


def test_command_first_run_returns_none(ctx):
    result = command("echo hello", ctx=ctx)
    assert result is None


def test_command_no_change(ctx):
    command("echo hello", ctx=ctx)
    result = command("echo hello", ctx=ctx)
    assert result is None


def test_command_detects_change(tmp_path, ctx):
    f = tmp_path / "val.txt"
    f.write_text("hello")
    cmd = f"cat {f}"
    command(cmd, ctx=ctx)
    f.write_text("world")
    result = command(cmd, ctx=ctx)
    assert result is not None
    assert "world" in result.after
    assert "hello" in result.before


# -- watch.url --


def test_url_first_run_returns_none(ctx):
    mock_resp = MagicMock()
    mock_resp.text = "<html>hello</html>"
    with patch("httpx.get", return_value=mock_resp) as mock_get:
        result = url("https://example.com", ctx=ctx)
    assert result is None
    mock_get.assert_called_once()


def test_url_no_change(ctx):
    mock_resp = MagicMock()
    mock_resp.text = "<html>hello</html>"
    with patch("httpx.get", return_value=mock_resp):
        url("https://example.com", ctx=ctx)
        result = url("https://example.com", ctx=ctx)
    assert result is None


def test_url_detects_change(ctx):
    resp1 = MagicMock()
    resp1.text = "<html>hello</html>"
    resp2 = MagicMock()
    resp2.text = "<html>goodbye</html>"
    with patch("httpx.get", side_effect=[resp1, resp2]):
        url("https://example.com", ctx=ctx)
        result = url("https://example.com", ctx=ctx)
    assert result is not None
    assert "goodbye" in result.after
    assert "hello" in result.before


def test_url_passes_headers(ctx):
    mock_resp = MagicMock()
    mock_resp.text = "ok"
    with patch("httpx.get", return_value=mock_resp) as mock_get:
        url("https://example.com", ctx=ctx, headers={"Authorization": "Bearer token"})
    mock_get.assert_called_once_with(
        "https://example.com",
        headers={"Authorization": "Bearer token"},
        follow_redirects=True,
        timeout=30,
    )


# -- state isolation --


def test_different_targets_independent_state(tmp_path, ctx):
    """Different watch targets use separate state keys."""
    f1 = tmp_path / "a.txt"
    f2 = tmp_path / "b.txt"
    f1.write_text("aaa")
    f2.write_text("bbb")

    file(str(f1), ctx=ctx)
    file(str(f2), ctx=ctx)

    f1.write_text("aaa-changed")
    result1 = file(str(f1), ctx=ctx)
    result2 = file(str(f2), ctx=ctx)

    assert result1 is not None
    assert result2 is None
