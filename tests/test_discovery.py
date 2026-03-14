from watchd.discovery import discover_agents


def test_discover_declarative(tmp_path):
    agents_dir = tmp_path / "watchd_agents"
    agents_dir.mkdir()
    (agents_dir / "hello.py").write_text(
        "schedule = '1h'\n"
        "agent = type('A', (), {'run': lambda self, msg, **kw: None})()\n"
        "prompt = 'say hi'\n"
    )
    agents = discover_agents(agents_dir)
    assert "hello" in agents
    assert agents["hello"].is_declarative


def test_discover_full_control(tmp_path):
    agents_dir = tmp_path / "watchd_agents"
    agents_dir.mkdir()
    (agents_dir / "worker.py").write_text("schedule = '5m'\n\ndef run():\n    return 'done'\n")
    agents = discover_agents(agents_dir)
    assert "worker" in agents
    assert not agents["worker"].is_declarative
    assert agents["worker"].run_fn() == "done"


def test_skip_no_schedule(tmp_path):
    agents_dir = tmp_path / "watchd_agents"
    agents_dir.mkdir()
    (agents_dir / "helper.py").write_text("x = 1\n")
    agents = discover_agents(agents_dir)
    assert agents == {}


def test_skip_underscore_files(tmp_path):
    agents_dir = tmp_path / "watchd_agents"
    agents_dir.mkdir()
    (agents_dir / "_helper.py").write_text("schedule = '1h'\ndef run(): pass\n")
    (agents_dir / "real.py").write_text("schedule = '1h'\ndef run(): pass\n")
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
    agents_dir = tmp_path / "watchd_agents"
    agents_dir.mkdir()
    (agents_dir / "bad.py").write_text("def broken(\n")
    (agents_dir / "good.py").write_text("schedule = '1h'\ndef run(): pass\n")
    agents = discover_agents(agents_dir)
    assert "good" in agents
    assert len(agents) == 1


def test_import_error_skipped(tmp_path):
    agents_dir = tmp_path / "watchd_agents"
    agents_dir.mkdir()
    (agents_dir / "broken.py").write_text("import nonexistent_package_xyz\n")
    (agents_dir / "ok.py").write_text("schedule = '1h'\ndef run(): pass\n")
    agents = discover_agents(agents_dir)
    assert "ok" in agents
    assert len(agents) == 1


def test_custom_dir_name(tmp_path):
    agents_dir = tmp_path / "my_agents"
    agents_dir.mkdir()
    (agents_dir / "task.py").write_text("schedule = '1h'\ndef run(): pass\n")
    agents = discover_agents(agents_dir)
    assert "task" in agents


def test_discover_directory_agent(tmp_path):
    agents_dir = tmp_path / "watchd_agents"
    sub = agents_dir / "analyzer"
    sub.mkdir(parents=True)
    (sub / "agent.py").write_text("schedule = '1h'\ndef run(): return 'ok'\n")
    agents = discover_agents(agents_dir)
    assert "analyzer" in agents


def test_skip_underscore_directory(tmp_path):
    agents_dir = tmp_path / "watchd_agents"
    sub = agents_dir / "_internal"
    sub.mkdir(parents=True)
    (sub / "agent.py").write_text("schedule = '1h'\ndef run(): pass\n")
    agents = discover_agents(agents_dir)
    assert agents == {}


def test_mixed_flat_and_directory(tmp_path):
    agents_dir = tmp_path / "watchd_agents"
    agents_dir.mkdir()
    (agents_dir / "simple.py").write_text("schedule = '1h'\ndef run(): pass\n")
    sub = agents_dir / "reporter"
    sub.mkdir()
    (sub / "agent.py").write_text("schedule = '2h'\ndef run(): pass\n")
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


def test_incomplete_agent_skipped(tmp_path):
    """Agent with schedule but no agent+prompt or run() is skipped."""
    agents_dir = tmp_path / "watchd_agents"
    agents_dir.mkdir()
    (agents_dir / "broken.py").write_text("schedule = '1h'\nx = 1\n")
    (agents_dir / "good.py").write_text("schedule = '1h'\ndef run(): pass\n")
    agents = discover_agents(agents_dir)
    assert "good" in agents
    assert len(agents) == 1


def test_custom_name(tmp_path):
    agents_dir = tmp_path / "watchd_agents"
    agents_dir.mkdir()
    (agents_dir / "myfile.py").write_text(
        "name = 'custom_name'\nschedule = '1h'\ndef run(): pass\n"
    )
    agents = discover_agents(agents_dir)
    assert "custom_name" in agents


def test_cron_schedule(tmp_path):
    agents_dir = tmp_path / "watchd_agents"
    agents_dir.mkdir()
    (agents_dir / "daily.py").write_text("schedule = '0 9 * * *'\ndef run(): pass\n")
    agents = discover_agents(agents_dir)
    assert "daily" in agents
    assert agents["daily"].schedule.trigger_type == "cron"
