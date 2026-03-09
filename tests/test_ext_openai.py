import sys
from unittest.mock import MagicMock, patch


def _mock_response(text="Hello from GPT"):
    choice = MagicMock()
    choice.message.content = text
    resp = MagicMock()
    resp.choices = [choice]
    return resp


def _make_mock_module():
    mock_mod = MagicMock()
    mock_mod.OpenAI.return_value.chat.completions.create.return_value = _mock_response()
    return mock_mod


def test_call_basic():
    mock_mod = _make_mock_module()
    with patch.dict(sys.modules, {"openai": mock_mod}):
        import importlib
        import watchd.ext.openai as mod

        importlib.reload(mod)
        result = mod.call(prompt="Hi")
    assert result == "Hello from GPT"
    mock_mod.OpenAI.return_value.chat.completions.create.assert_called_once()


def test_call_with_system():
    mock_mod = _make_mock_module()
    with patch.dict(sys.modules, {"openai": mock_mod}):
        import importlib
        import watchd.ext.openai as mod

        importlib.reload(mod)
        mod.call(prompt="Hi", system="Be helpful")
    kwargs = mock_mod.OpenAI.return_value.chat.completions.create.call_args.kwargs
    messages = kwargs["messages"]
    assert len(messages) == 2
    assert messages[0] == {"role": "system", "content": "Be helpful"}
    assert messages[1] == {"role": "user", "content": "Hi"}


def test_call_no_system():
    mock_mod = _make_mock_module()
    with patch.dict(sys.modules, {"openai": mock_mod}):
        import importlib
        import watchd.ext.openai as mod

        importlib.reload(mod)
        mod.call(prompt="Hi")
    kwargs = mock_mod.OpenAI.return_value.chat.completions.create.call_args.kwargs
    messages = kwargs["messages"]
    assert len(messages) == 1
    assert messages[0] == {"role": "user", "content": "Hi"}


def test_import_error():
    with patch.dict(sys.modules, {"openai": None}):
        import importlib
        import watchd.ext.openai as mod

        importlib.reload(mod)
        try:
            mod.call(prompt="Hi")
            assert False, "Should have raised"
        except (ImportError, ModuleNotFoundError):
            pass
