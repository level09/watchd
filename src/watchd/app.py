"""Watchd - main entry point. Agent registration, scheduler lifecycle, storage init."""

from __future__ import annotations

import signal
import sys

import structlog

from watchd.agent import Agent
from watchd.runner import execute_agent, install_capture, uninstall_capture
from watchd.schedule import Schedule
from watchd.store import Store

log = structlog.get_logger()


class Watchd:
    def __init__(self, db: str = "./watchd.db"):
        self.store = Store(db)
        self.agents: dict[str, Agent] = {}
        self.scheduler = None

    def agent(self, schedule: Schedule | None = None, name: str | None = None, retries: int = 0):
        """Decorator to register an agent."""

        def decorator(fn):
            agent_name = name or fn.__name__
            self.agents[agent_name] = Agent(
                name=agent_name, fn=fn, schedule=schedule, retries=retries
            )
            return fn

        return decorator

    def start(self):
        """Start scheduler and block."""
        from apscheduler.schedulers.blocking import BlockingScheduler

        install_capture()
        self.store.init()
        self._sync_agents()

        self.scheduler = BlockingScheduler()

        for agent in self.agents.values():
            if agent.schedule:
                trigger = agent.schedule.to_apscheduler_trigger()
                self.scheduler.add_job(
                    self._execute,
                    trigger=trigger,
                    args=[agent.name],
                    id=agent.name,
                    replace_existing=True,
                )
                log.info("agent_scheduled", agent=agent.name, schedule=str(agent.schedule))

        def _shutdown(signum, frame):
            log.info("shutting_down")
            if self.scheduler:
                self.scheduler.shutdown(wait=False)
            uninstall_capture()
            self.store.close()
            sys.exit(0)

        signal.signal(signal.SIGINT, _shutdown)
        signal.signal(signal.SIGTERM, _shutdown)

        log.info("watchd_started", agents=len(self.agents))
        self.scheduler.start()

    def run(self, agent_name: str):
        """Run one agent immediately."""
        install_capture()
        try:
            self.store.init()
            self._sync_agents()
            return self._execute(agent_name)
        finally:
            uninstall_capture()

    def _execute(self, agent_name: str):
        agent = self.agents.get(agent_name)
        if agent is None:
            raise KeyError(f"Agent '{agent_name}' not found")
        return execute_agent(agent, self.store)

    def _sync_agents(self):
        for agent in self.agents.values():
            schedule_str = str(agent.schedule) if agent.schedule else None
            self.store.sync_agent(agent.name, schedule_str, agent.retries)
