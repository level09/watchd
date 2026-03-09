import pytest

from watchd.schedule import Schedule, every, parse_schedule


def test_every_hour():
    s = every.hour
    assert s == Schedule("interval", {"hours": 1})


def test_every_minutes():
    s = every.minutes(30)
    assert s == Schedule("interval", {"minutes": 30})


def test_every_seconds():
    s = every.seconds(10)
    assert s == Schedule("interval", {"seconds": 10})


def test_every_day_at():
    s = every.day.at("03:00")
    assert s == Schedule("cron", {"hour": 3, "minute": 0})


def test_every_monday_at():
    s = every.monday.at("09:00")
    assert s == Schedule("cron", {"day_of_week": "mon", "hour": 9, "minute": 0})


def test_every_cron():
    s = every.cron("*/5 * * * *")
    assert s == Schedule("cron", {"crontab": "*/5 * * * *"})


def test_to_apscheduler_interval():
    from apscheduler.triggers.interval import IntervalTrigger

    trigger = every.minutes(15).to_apscheduler_trigger()
    assert isinstance(trigger, IntervalTrigger)


def test_to_apscheduler_cron():
    from apscheduler.triggers.cron import CronTrigger

    trigger = every.day.at("14:30").to_apscheduler_trigger()
    assert isinstance(trigger, CronTrigger)


def test_to_apscheduler_crontab():
    from apscheduler.triggers.cron import CronTrigger

    trigger = every.cron("0 3 * * *").to_apscheduler_trigger()
    assert isinstance(trigger, CronTrigger)


def test_str_interval():
    assert "every" in str(every.hour)


def test_str_cron():
    assert "cron" in str(every.cron("0 * * * *"))


# parse_schedule tests


def test_parse_seconds():
    assert parse_schedule("30s") == Schedule("interval", {"seconds": 30})


def test_parse_minutes():
    assert parse_schedule("5m") == Schedule("interval", {"minutes": 5})


def test_parse_hours():
    assert parse_schedule("2h") == Schedule("interval", {"hours": 2})


def test_parse_days():
    assert parse_schedule("1d") == Schedule("interval", {"days": 1})


def test_parse_cron():
    assert parse_schedule("*/5 * * * *") == Schedule("cron", {"crontab": "*/5 * * * *"})


def test_parse_cron_daily():
    assert parse_schedule("0 9 * * MON-FRI") == Schedule("cron", {"crontab": "0 9 * * MON-FRI"})


def test_parse_whitespace():
    assert parse_schedule("  5m  ") == Schedule("interval", {"minutes": 5})


def test_parse_invalid():
    with pytest.raises(ValueError, match="Invalid schedule"):
        parse_schedule("5x")


def test_parse_invalid_no_unit():
    with pytest.raises(ValueError, match="Invalid schedule"):
        parse_schedule("fast")
