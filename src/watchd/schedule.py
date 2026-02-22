"""Fluent scheduling DSL. Produces plain data, no APScheduler import at build time."""

from __future__ import annotations

from dataclasses import dataclass, field


@dataclass(frozen=True)
class Schedule:
    trigger_type: str  # "interval" or "cron"
    kwargs: dict = field(default_factory=dict)

    def to_apscheduler_trigger(self):
        from apscheduler.triggers.cron import CronTrigger
        from apscheduler.triggers.interval import IntervalTrigger

        if self.trigger_type == "interval":
            return IntervalTrigger(**self.kwargs)
        if self.trigger_type == "cron":
            if "crontab" in self.kwargs:
                return CronTrigger.from_crontab(self.kwargs["crontab"])
            return CronTrigger(**self.kwargs)
        raise ValueError(f"Unknown trigger type: {self.trigger_type}")

    def __str__(self):
        if self.trigger_type == "interval":
            parts = [f"{v}{k[0]}" for k, v in self.kwargs.items()]
            return f"every {' '.join(parts)}"
        if self.trigger_type == "cron" and "crontab" in self.kwargs:
            return f"cron({self.kwargs['crontab']})"
        return f"cron({self.kwargs})"


class _DayOfWeekBuilder:
    """Intermediate builder for every.monday.at(...) etc."""

    def __init__(self, day_of_week: str):
        self._dow = day_of_week

    def at(self, time_str: str) -> Schedule:
        hour, minute = _parse_time(time_str)
        return Schedule("cron", {"day_of_week": self._dow, "hour": hour, "minute": minute})


class _DayBuilder:
    """Intermediate builder for every.day.at(...)."""

    def at(self, time_str: str) -> Schedule:
        hour, minute = _parse_time(time_str)
        return Schedule("cron", {"hour": hour, "minute": minute})


_DAYS = {
    "monday": "mon",
    "tuesday": "tue",
    "wednesday": "wed",
    "thursday": "thu",
    "friday": "fri",
    "saturday": "sat",
    "sunday": "sun",
}


class _Every:
    """Module-level singleton providing the fluent schedule API."""

    @property
    def hour(self) -> Schedule:
        return Schedule("interval", {"hours": 1})

    @property
    def day(self) -> _DayBuilder:
        return _DayBuilder()

    def __getattr__(self, name: str):
        if name in _DAYS:
            return _DayOfWeekBuilder(_DAYS[name])
        raise AttributeError(f"'every' has no attribute '{name}'")

    def minutes(self, n: int) -> Schedule:
        return Schedule("interval", {"minutes": n})

    def seconds(self, n: int) -> Schedule:
        return Schedule("interval", {"seconds": n})

    def hours(self, n: int) -> Schedule:
        return Schedule("interval", {"hours": n})

    def cron(self, expression: str) -> Schedule:
        return Schedule("cron", {"crontab": expression})


def _parse_time(time_str: str) -> tuple[int, int]:
    parts = time_str.split(":")
    if len(parts) != 2:
        raise ValueError(f"Expected HH:MM format, got: {time_str}")
    return int(parts[0]), int(parts[1])


every = _Every()
