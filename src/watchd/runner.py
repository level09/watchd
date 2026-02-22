"""Execution engine. Runs agents, tracks runs."""

from __future__ import annotations

import io
import sys
import threading
import traceback
from datetime import datetime, timezone
from uuid import uuid4

import structlog

from watchd.agent import Agent, AgentContext
from watchd.store import Run, Store

_local = threading.local()


class _ThreadSafeStream:
    """Wraps sys.stdout so each thread can capture to its own buffer.

    When _local.buf is set (during agent execution), output goes to that
    buffer AND to the original stream. When unset, output goes only to
    the original stream. Thread-safe because threading.local gives each
    thread its own buf attribute.
    """

    def __init__(self, original):
        self._original = original

    def write(self, s):
        buf = getattr(_local, "buf", None)
        if buf is not None:
            buf.write(s)
        return self._original.write(s)

    def flush(self):
        self._original.flush()

    def __getattr__(self, name):
        return getattr(self._original, name)


def _install_capture():
    if not isinstance(sys.stdout, _ThreadSafeStream):
        sys.stdout = _ThreadSafeStream(sys.stdout)


def execute_agent(agent: Agent, store: Store) -> Run:
    _install_capture()

    run_id = uuid4().hex[:12]
    log = structlog.get_logger().bind(agent=agent.name, run_id=run_id)
    ctx = AgentContext(agent.name, run_id, store, log)

    now = datetime.now(timezone.utc)
    run = Run(id=run_id, agent=agent.name, status="running", started_at=now)
    store.save_run(run)

    attempts = 1 + agent.retries
    last_error = None

    buf = io.StringIO()
    _local.buf = buf

    try:
        for attempt in range(1, attempts + 1):
            try:
                result = agent.fn(ctx)
                run.status = "success"
                run.result = str(result) if result is not None else None
                last_error = None
                break
            except Exception as e:
                last_error = e
                if attempt < attempts:
                    log.warning("agent_retry", attempt=attempt, error=str(e))
                else:
                    log.error("agent_failed", error=str(e))

        if last_error:
            run.status = "error"
            run.error = traceback.format_exception(last_error)[-1].strip()
    except BaseException as e:
        run.status = "error"
        run.error = f"{type(e).__name__}: {e}"
        raise
    finally:
        _local.buf = None
        ctx.state.flush()
        run.output = buf.getvalue() or None
        run.finished_at = datetime.now(timezone.utc)
        run.duration_ms = (run.finished_at - run.started_at).total_seconds() * 1000
        store.update_run(run)
        log.info("agent_finished", status=run.status, result=run.result, duration_ms=round(run.duration_ms))

    return run
