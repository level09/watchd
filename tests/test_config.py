import pytest

from watchd.config import Config, load_config


def test_defaults():
    c = Config()
    assert c.db == "./watchd.db"
    assert c.agents_dir == "watchd_agents"
    assert c.log_level == "info"
    assert c.timezone == "UTC"


def test_load_missing_file(tmp_path):
    c = load_config(tmp_path / "nope.toml")
    assert c == Config()


def test_load_from_file(tmp_path):
    toml = tmp_path / "watchd.toml"
    toml.write_text('[watchd]\ndb = "./custom.db"\nagents_dir = "my_agents"\nlog_level = "debug"\n')
    c = load_config(toml)
    assert c.db == "./custom.db"
    assert c.agents_dir == "my_agents"
    assert c.log_level == "debug"
    assert c.timezone == "UTC"  # default preserved


def test_load_partial(tmp_path):
    toml = tmp_path / "watchd.toml"
    toml.write_text('[watchd]\ndb = "./other.db"\n')
    c = load_config(toml)
    assert c.db == "./other.db"
    assert c.agents_dir == "watchd_agents"


def test_malformed_toml_exits(tmp_path):
    toml = tmp_path / "watchd.toml"
    toml.write_text("this is not valid toml [[[")
    with pytest.raises(SystemExit):
        load_config(toml)


def test_empty_watchd_section(tmp_path):
    toml = tmp_path / "watchd.toml"
    toml.write_text("[watchd]\n")
    c = load_config(toml)
    assert c == Config()


def test_unknown_keys_ignored(tmp_path):
    """Extra keys don't crash, they're just ignored."""
    toml = tmp_path / "watchd.toml"
    toml.write_text('[watchd]\ndb = "./x.db"\ntypo_key = "oops"\n')
    c = load_config(toml)
    assert c.db == "./x.db"
