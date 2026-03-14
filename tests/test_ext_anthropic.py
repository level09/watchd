import sys
from unittest.mock import MagicMock, patch


def _mock_response(text="Hello from Claude"):
    block = MagicMock()
    block.text = text
    msg = MagicMock()
    msg.content = [block]
    return msg


def _make_mock_module():
    mock_mod = MagicMock()
    mock_mod.Anthropic.return_value.messages.create.return_value = _mock_response()
    return mock_mod


def test_call_basic():
    mock_mod = _make_mock_module()
    with patch.dict(sys.modules, {"anthropic": mock_mod}):
        import importlib
        import watchd.ext.anthropic as mod

        importlib.reload(mod)
        result = mod.call(prompt="Hi")
    assert result == "Hello from Claude"
    mock_mod.Anthropic.return_value.messages.create.assert_called_once()


def test_call_passes_kwargs():
    mock_mod = _make_mock_module()
    with patch.dict(sys.modules, {"anthropic": mock_mod}):
        import importlib
        import watchd.ext.anthropic as mod

        importlib.reload(mod)
        mod.call(
            prompt="Hi",
            system="Be helpful",
            model="claude-opus-4-20250514",
            max_tokens=1024,
            temperature=0.5,
        )
    kwargs = mock_mod.Anthropic.return_value.messages.create.call_args.kwargs
    assert kwargs["model"] == "claude-opus-4-20250514"
    assert kwargs["max_tokens"] == 1024
    assert kwargs["system"] == [{"type": "text", "text": "Be helpful"}]
    assert kwargs["temperature"] == 0.5


def test_call_no_system():
    mock_mod = _make_mock_module()
    with patch.dict(sys.modules, {"anthropic": mock_mod}):
        import importlib
        import watchd.ext.anthropic as mod

        importlib.reload(mod)
        mod.call(prompt="Hi")
    kwargs = mock_mod.Anthropic.return_value.messages.create.call_args.kwargs
    assert "system" not in kwargs


def test_import_error():
    with patch.dict(sys.modules, {"anthropic": None}):
        import importlib
        import watchd.ext.anthropic as mod

        importlib.reload(mod)
        try:
            mod.call(prompt="Hi")
            assert False, "Should have raised"
        except (ImportError, ModuleNotFoundError):
            pass
