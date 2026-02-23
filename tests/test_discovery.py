from watchd.discovery import discover_agents
from watchd.registry import clear_registry


def setup_function():
    clear_registry()


def test_discover_from_dir(tmp_path):
    agents_dir = tmp_path / "watchd_agents"
    agents_dir.mkdir()
    (agents_dir / "hello.py").write_text(
        "from watchd import agent, every\n\n"
        "@agent(schedule=every.hour)\n"
        "def hello(ctx):\n"
        "    return 'hi'\n"
    )

    agents = discover_agents(agents_dir)
    assert "hello" in agents
    assert agents["hello"].fn is not None


def test_skip_underscore_files(tmp_path):
    agents_dir = tmp_path / "watchd_agents"
    agents_dir.mkdir()
    (agents_dir / "_helper.py").write_text("x = 1\n")
    (agents_dir / "real.py").write_text(
        "from watchd import agent\n\n@agent()\ndef real(ctx):\n    pass\n"
    )

    agents = discover_agents(agents_dir)
    assert "real" in agents
    assert len(agents) == 1


def test_nonexistent_dir(tmp_path):
    agents = discover_agents(tmp_path / "nope")
    assert agents == {}


def test_empty_dir(tmp_path):
    agents_dir = tmp_path / "watchd_agents"
    agents_dir.mkdir()
    agents = discover_agents(agents_dir)
    assert agents == {}


def test_syntax_error_skipped(tmp_path):
    """Agent files with syntax errors are skipped, not fatal."""
    agents_dir = tmp_path / "watchd_agents"
    agents_dir.mkdir()
    (agents_dir / "bad.py").write_text("def broken(\n")
    (agents_dir / "good.py").write_text(
        "from watchd import agent\n\n@agent()\ndef good(ctx):\n    pass\n"
    )

    agents = discover_agents(agents_dir)
    assert "good" in agents
    assert len(agents) == 1


def test_import_error_skipped(tmp_path):
    """Agent files with import errors are skipped."""
    agents_dir = tmp_path / "watchd_agents"
    agents_dir.mkdir()
    (agents_dir / "broken.py").write_text("import nonexistent_package_xyz\n")
    (agents_dir / "ok.py").write_text(
        "from watchd import agent\n\n@agent()\ndef ok(ctx):\n    pass\n"
    )

    agents = discover_agents(agents_dir)
    assert "ok" in agents
    assert len(agents) == 1


def test_custom_dir_name(tmp_path):
    """Module prefix should match directory name, not be hardcoded."""
    agents_dir = tmp_path / "my_agents"
    agents_dir.mkdir()
    (agents_dir / "task.py").write_text(
        "from watchd import agent\n\n@agent()\ndef task(ctx):\n    pass\n"
    )

    agents = discover_agents(agents_dir)
    assert "task" in agents


def test_discover_directory_agent(tmp_path):
    agents_dir = tmp_path / "watchd_agents"
    sub = agents_dir / "analyzer"
    sub.mkdir(parents=True)
    (sub / "agent.py").write_text(
        "from watchd import agent, every\n\n"
        "@agent(schedule=every.hour)\n"
        "def analyzer(ctx):\n"
        "    return 'ok'\n"
    )

    agents = discover_agents(agents_dir)
    assert "analyzer" in agents


def test_skip_underscore_directory(tmp_path):
    agents_dir = tmp_path / "watchd_agents"
    sub = agents_dir / "_internal"
    sub.mkdir(parents=True)
    (sub / "agent.py").write_text(
        "from watchd import agent\n\n@agent()\ndef hidden(ctx):\n    pass\n"
    )

    agents = discover_agents(agents_dir)
    assert agents == {}


def test_mixed_flat_and_directory(tmp_path):
    agents_dir = tmp_path / "watchd_agents"
    agents_dir.mkdir()
    (agents_dir / "simple.py").write_text(
        "from watchd import agent\n\n@agent()\ndef simple(ctx):\n    pass\n"
    )
    sub = agents_dir / "reporter"
    sub.mkdir()
    (sub / "agent.py").write_text(
        "from watchd import agent\n\n@agent()\ndef reporter(ctx):\n    pass\n"
    )

    agents = discover_agents(agents_dir)
    assert "simple" in agents
    assert "reporter" in agents
    assert len(agents) == 2


def test_directory_without_agent_py(tmp_path):
    agents_dir = tmp_path / "watchd_agents"
    data = agents_dir / "data"
    data.mkdir(parents=True)
    (data / "notes.txt").write_text("not an agent")

    agents = discover_agents(agents_dir)
    assert agents == {}
