from watchd.registry import agent, clear_registry, get_registry
from watchd.schedule import Schedule, every


def setup_function():
    clear_registry()


def test_decorator_registers():
    @agent(schedule=every.hour)
    def my_agent(ctx):
        return "done"

    reg = get_registry()
    assert "my_agent" in reg
    assert reg["my_agent"].schedule == every.hour
    assert reg["my_agent"].fn is my_agent


def test_custom_name():
    @agent(schedule=every.minutes(5), name="custom")
    def whatever(ctx):
        return "ok"

    reg = get_registry()
    assert "custom" in reg
    assert "whatever" not in reg


def test_clear():
    @agent()
    def temp(ctx):
        pass

    assert "temp" in get_registry()
    clear_registry()
    assert len(get_registry()) == 0


def test_retries():
    @agent(retries=3)
    def flaky(ctx):
        pass

    assert get_registry()["flaky"].retries == 3


def test_duplicate_name_overwrites_with_warning():
    @agent(name="dup")
    def first(ctx):
        pass

    @agent(name="dup")
    def second(ctx):
        pass

    reg = get_registry()
    assert reg["dup"].fn is second


def test_every_string():
    @agent(every="5m")
    def fast(ctx):
        pass

    reg = get_registry()
    assert reg["fast"].schedule == Schedule("interval", {"minutes": 5})


def test_every_string_cron():
    @agent(every="0 9 * * *")
    def daily(ctx):
        pass

    reg = get_registry()
    assert reg["daily"].schedule == Schedule("cron", {"crontab": "0 9 * * *"})


def test_agno_params():
    @agent(
        every="1h",
        model="anthropic:claude-sonnet-4-5-20250929",
        instructions=["Do stuff", "Be smart"],
        learning=True,
    )
    def smart(ctx):
        pass

    entry = get_registry()["smart"]
    assert entry.model == "anthropic:claude-sonnet-4-5-20250929"
    assert entry.instructions == ["Do stuff", "Be smart"]
    assert entry.learning is True


def test_instructions_string_to_list():
    @agent(instructions="Single instruction")
    def single(ctx):
        pass

    entry = get_registry()["single"]
    assert entry.instructions == ["Single instruction"]


def test_description_from_docstring():
    @agent()
    def documented(ctx):
        """This agent does cool things."""
        pass

    entry = get_registry()["documented"]
    assert entry.description == "This agent does cool things."
