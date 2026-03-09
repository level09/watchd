"""Tests for the AI judge pattern."""

from unittest.mock import patch

import pytest

from watchd.ext.judge import Verdict, judge
from watchd.watch import Change


@pytest.fixture
def change():
    return Change(
        before="price: $10",
        after="price: $15",
        diff="-price: $10\n+price: $15",
        summary="1 added, 1 removed",
    )


def test_judge_should_act(change):
    with patch(
        "watchd.ext.anthropic.call", return_value="ACT: yes\nSUMMARY: Price increased by 50%"
    ):
        result = judge(change, instruction="Did the price change?")
    assert isinstance(result, Verdict)
    assert result.should_act is True
    assert "Price increased" in result.summary


def test_judge_should_not_act(change):
    with patch(
        "watchd.ext.anthropic.call", return_value="ACT: no\nSUMMARY: Minor formatting change"
    ):
        result = judge(change, instruction="Anything important?")
    assert result.should_act is False
    assert "formatting" in result.summary


def test_judge_openai_provider(change):
    with patch("watchd.ext.openai.call", return_value="ACT: yes\nSUMMARY: Important change"):
        result = judge(change, instruction="Check this", provider="openai")
    assert result.should_act is True


def test_judge_unknown_provider(change):
    with pytest.raises(ValueError, match="Unknown provider"):
        judge(change, instruction="test", provider="gemini")


def test_judge_custom_template(change):
    template = "Change: {diff}\nQ: {instruction}\nAnswer ACT: yes/no and SUMMARY:"
    with patch("watchd.ext.anthropic.call", return_value="ACT: no\nSUMMARY: Not relevant") as mock:
        judge(change, instruction="Is this important?", template=template)
    prompt = mock.call_args.kwargs["prompt"]
    assert "Change:" in prompt
    assert "Q:" in prompt


def test_judge_truncates_diff():
    long_diff = "x" * 5000
    change = Change(before="", after="", diff=long_diff, summary="big change")
    with patch("watchd.ext.anthropic.call", return_value="ACT: no\nSUMMARY: Too much") as mock:
        judge(change, instruction="Check")
    prompt = mock.call_args.kwargs["prompt"]
    assert len(long_diff[:4000]) == 4000
    assert "x" * 4000 in prompt
    assert "x" * 4001 not in prompt


def test_judge_no_summary_line(change):
    with patch("watchd.ext.anthropic.call", return_value="ACT: yes\nThis is important"):
        result = judge(change, instruction="Check")
    assert result.should_act is True
    assert result.summary == "ACT: yes\nThis is important"[:200]


def test_judge_passes_kwargs(change):
    with patch("watchd.ext.anthropic.call", return_value="ACT: no\nSUMMARY: ok") as mock:
        judge(change, instruction="Check", model="claude-haiku-4-5-20251001", max_tokens=100)
    mock.assert_called_once()
    assert mock.call_args.kwargs["model"] == "claude-haiku-4-5-20251001"
    assert mock.call_args.kwargs["max_tokens"] == 100
