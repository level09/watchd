"""Execution engine. Runs discovered agents, tracks runs."""

from __future__ import annotations

import io
import sys
import threading
import traceback
from datetime import datetime, timezone
from uuid import uuid4

import structlog

from watchd.discovery import AgentEntry
from watchd.store import Run, Store

_local = threading.local()
_original_stdout = None


class _ThreadSafeStream:
    def __init__(self, original):
        self._original = original

    def write(self, s):
        buf = getattr(_local, "buf", None)
        if buf is not None:
            buf.write(s)
        return self._original.write(s)

    def writelines(self, lines):
        for line in lines:
            self.write(line)

    def flush(self):
        self._original.flush()

    def __getattr__(self, name):
        return getattr(self._original, name)


def install_capture():
    global _original_stdout
    if not isinstance(sys.stdout, _ThreadSafeStream):
        _original_stdout = sys.stdout
        sys.stdout = _ThreadSafeStream(sys.stdout)


def uninstall_capture():
    global _original_stdout
    if _original_stdout is not None:
        sys.stdout = _original_stdout
        _original_stdout = None


def execute_agent(entry: AgentEntry, store: Store) -> Run:
    install_capture()

    run_id = uuid4().hex[:12]
    log = structlog.get_logger().bind(agent=entry.name, run_id=run_id)

    now = datetime.now(timezone.utc)
    run = Run(id=run_id, agent=entry.name, status="running", started_at=now)
    store.save_run(run)

    buf = io.StringIO()
    _local.buf = buf

    try:
        if entry.is_declarative:
            # Declarative: agent + prompt defined at module level
            response = entry.agno_agent.run(
                entry.prompt,
                session_id=entry.name,
                user_id=entry.name,
            )
            run.result = str(response.content) if response.content else None
        else:
            # Full control: user-defined run() function
            result = entry.run_fn()
            if hasattr(result, "content"):
                run.result = str(result.content)
            else:
                run.result = str(result) if result is not None else None

        run.status = "success"

    except Exception as e:
        log.error("agent_failed", error=str(e))
        run.status = "error"
        run.error = "".join(traceback.format_exception(e)).strip()

    except BaseException as e:
        run.status = "error"
        run.error = f"{type(e).__name__}: {e}"
        raise

    finally:
        _local.buf = None
        run.output = buf.getvalue() or None
        run.finished_at = datetime.now(timezone.utc)
        run.duration_ms = (run.finished_at - run.started_at).total_seconds() * 1000
        store.update_run(run)
        log.info(
            "agent_finished",
            status=run.status,
            duration_ms=round(run.duration_ms),
        )

    return run
