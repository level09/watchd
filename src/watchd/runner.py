"""Execution engine. Builds Agno agents from registry entries, runs them, tracks runs."""

from __future__ import annotations

import inspect
import io
import sys
import threading
import traceback
from dataclasses import dataclass
from datetime import datetime, timezone
from uuid import uuid4

import structlog

from watchd.registry import AgentEntry
from watchd.store import Run, Store

_local = threading.local()
_original_stdout = None


@dataclass
class AgentContext:
    """Passed to full-mode agent functions."""

    agent_name: str
    run_id: str
    db: object  # agno SqliteDb instance
    log: object


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


def _build_agno_agent(entry: AgentEntry, db):
    """Build an Agno Agent from registry metadata."""
    from agno.agent import Agent

    model = None
    if entry.model:
        model = _resolve_model(entry.model)

    kwargs = {
        "name": entry.name,
        "tools": entry.tools or [],
        "instructions": entry.instructions or [],
        "markdown": False,
        "retries": entry.retries,
    }

    if model:
        kwargs["model"] = model

    if entry.output_schema:
        kwargs["output_schema"] = entry.output_schema

    if db:
        kwargs["db"] = db

    if entry.learning:
        kwargs["learning"] = True

    if entry.description:
        kwargs["description"] = entry.description

    return Agent(**kwargs)


def _resolve_model(model_str: str):
    """Resolve 'provider:model_id' string to an Agno model instance."""
    if ":" not in model_str:
        # Default to anthropic
        from agno.models.anthropic import Claude

        return Claude(id=model_str)

    provider, model_id = model_str.split(":", 1)

    if provider == "anthropic":
        from agno.models.anthropic import Claude

        return Claude(id=model_id)
    elif provider == "openai":
        from agno.models.openai import OpenAIChat

        return OpenAIChat(id=model_id)
    elif provider == "google":
        from agno.models.google import Gemini

        return Gemini(id=model_id)
    else:
        raise ValueError(f"Unknown model provider: {provider}")


def _is_full_mode(fn) -> bool:
    """Check if the function expects a ctx argument (full mode) vs simple mode."""
    sig = inspect.signature(fn)
    return len(sig.parameters) > 0


def execute_agent(entry: AgentEntry, store: Store, agno_db=None) -> Run:
    install_capture()

    run_id = uuid4().hex[:12]
    log = structlog.get_logger().bind(agent=entry.name, run_id=run_id)

    now = datetime.now(timezone.utc)
    run = Run(id=run_id, agent=entry.name, status="running", started_at=now)
    store.save_run(run)

    buf = io.StringIO()
    _local.buf = buf

    try:
        if _is_full_mode(entry.fn):
            # Full mode: user builds their own Agno agent
            ctx = AgentContext(
                agent_name=entry.name,
                run_id=run_id,
                db=agno_db,
                log=log,
            )
            result = entry.fn(ctx)
            if hasattr(result, "content"):
                run.result = str(result.content)
            else:
                run.result = str(result) if result is not None else None
        else:
            # Simple mode: function returns a prompt, we build the Agno agent
            prompt = entry.fn()
            if prompt and (entry.model or entry.tools or entry.instructions):
                agno_agent = _build_agno_agent(entry, agno_db)
                response = agno_agent.run(str(prompt))
                run.result = str(response.content) if response.content else None
            else:
                run.result = str(prompt) if prompt is not None else None

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
