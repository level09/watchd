from watchd.registry import agent, clear_registry, get_registry
from watchd.schedule import every


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
