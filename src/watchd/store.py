"""SQLite storage layer. Zero ORM, raw sqlite3 + dataclasses."""

from __future__ import annotations

import json
import sqlite3
import threading
from dataclasses import dataclass
from datetime import datetime

_SCHEMA = """
CREATE TABLE IF NOT EXISTS agents (
    name TEXT PRIMARY KEY,
    schedule TEXT,
    retries INTEGER DEFAULT 0,
    created_at TEXT DEFAULT (strftime('%Y-%m-%dT%H:%M:%fZ', 'now')),
    updated_at TEXT DEFAULT (strftime('%Y-%m-%dT%H:%M:%fZ', 'now'))
);

CREATE TABLE IF NOT EXISTS runs (
    id TEXT PRIMARY KEY,
    agent TEXT NOT NULL REFERENCES agents(name),
    status TEXT NOT NULL DEFAULT 'running',
    result TEXT,
    output TEXT,
    error TEXT,
    started_at TEXT NOT NULL,
    finished_at TEXT,
    duration_ms REAL,
    created_at TEXT DEFAULT (strftime('%Y-%m-%dT%H:%M:%fZ', 'now'))
);

CREATE INDEX IF NOT EXISTS idx_runs_agent ON runs(agent, started_at DESC);

CREATE TABLE IF NOT EXISTS agent_state (
    agent TEXT NOT NULL REFERENCES agents(name),
    key TEXT NOT NULL,
    value TEXT,
    updated_at TEXT DEFAULT (strftime('%Y-%m-%dT%H:%M:%fZ', 'now')),
    PRIMARY KEY (agent, key)
);
"""


@dataclass
class Run:
    id: str
    agent: str
    status: str = "running"
    result: str | None = None
    output: str | None = None
    error: str | None = None
    started_at: datetime | None = None
    finished_at: datetime | None = None
    duration_ms: float | None = None


class Store:
    def __init__(self, db_path: str):
        self.db_path = db_path
        self._local = threading.local()

    @property
    def conn(self) -> sqlite3.Connection:
        c = getattr(self._local, "conn", None)
        if c is None:
            c = sqlite3.connect(self.db_path)
            c.row_factory = sqlite3.Row
            c.execute("PRAGMA journal_mode=WAL")
            c.execute("PRAGMA foreign_keys=ON")
            self._local.conn = c
        return c

    def init(self):
        self.conn.executescript(_SCHEMA)

    def sync_agent(self, name: str, schedule_str: str | None = None, retries: int = 0):
        self.conn.execute(
            """INSERT INTO agents (name, schedule, retries) VALUES (?, ?, ?)
               ON CONFLICT(name) DO UPDATE SET
                 schedule = excluded.schedule,
                 retries = excluded.retries,
                 updated_at = strftime('%Y-%m-%dT%H:%M:%fZ', 'now')""",
            (name, schedule_str, retries),
        )
        self.conn.commit()

    def save_run(self, run: Run):
        self.conn.execute(
            """INSERT INTO runs (id, agent, status, result, output, error, started_at, finished_at, duration_ms)
               VALUES (?, ?, ?, ?, ?, ?, ?, ?, ?)""",
            (
                run.id,
                run.agent,
                run.status,
                run.result,
                run.output,
                run.error,
                _to_iso(run.started_at),
                _to_iso(run.finished_at),
                run.duration_ms,
            ),
        )
        self.conn.commit()

    def update_run(self, run: Run):
        self.conn.execute(
            """UPDATE runs SET status=?, result=?, output=?, error=?, finished_at=?, duration_ms=?
               WHERE id=?""",
            (
                run.status,
                run.result,
                run.output,
                run.error,
                _to_iso(run.finished_at),
                run.duration_ms,
                run.id,
            ),
        )
        self.conn.commit()

    def get_run(self, run_id: str) -> Run | None:
        row = self.conn.execute("SELECT * FROM runs WHERE id=?", (run_id,)).fetchone()
        return _row_to_run(row) if row else None

    def get_runs(self, agent_name: str, limit: int = 20) -> list[Run]:
        rows = self.conn.execute(
            "SELECT * FROM runs WHERE agent=? ORDER BY started_at DESC LIMIT ?",
            (agent_name, limit),
        ).fetchall()
        return [_row_to_run(r) for r in rows]

    def get_all_runs(self, limit: int = 20) -> list[Run]:
        rows = self.conn.execute(
            "SELECT * FROM runs ORDER BY started_at DESC LIMIT ?", (limit,)
        ).fetchall()
        return [_row_to_run(r) for r in rows]

    def get_state(self, agent_name: str) -> dict[str, object]:
        rows = self.conn.execute(
            "SELECT key, value FROM agent_state WHERE agent=?", (agent_name,)
        ).fetchall()
        return {r["key"]: json.loads(r["value"]) for r in rows}

    def set_state(self, agent_name: str, key: str, value: object):
        self.conn.execute(
            """INSERT INTO agent_state (agent, key, value) VALUES (?, ?, ?)
               ON CONFLICT(agent, key) DO UPDATE SET
                 value = excluded.value,
                 updated_at = strftime('%Y-%m-%dT%H:%M:%fZ', 'now')""",
            (agent_name, key, json.dumps(value)),
        )
        self.conn.commit()

    def set_state_bulk(self, agent_name: str, data: dict[str, object]):
        with self.conn:
            for key, value in data.items():
                self.conn.execute(
                    """INSERT INTO agent_state (agent, key, value) VALUES (?, ?, ?)
                       ON CONFLICT(agent, key) DO UPDATE SET
                         value = excluded.value,
                         updated_at = strftime('%Y-%m-%dT%H:%M:%fZ', 'now')""",
                    (agent_name, key, json.dumps(value)),
                )

    def delete_state_keys(self, agent_name: str, keys: set[str]):
        with self.conn:
            for key in keys:
                self.conn.execute(
                    "DELETE FROM agent_state WHERE agent=? AND key=?", (agent_name, key)
                )

    def get_all_agents(self) -> list[dict]:
        rows = self.conn.execute("SELECT * FROM agents ORDER BY name").fetchall()
        return [dict(r) for r in rows]

    def close(self):
        c = getattr(self._local, "conn", None)
        if c:
            c.close()
            self._local.conn = None


def _to_iso(dt: datetime | None) -> str | None:
    if dt is None:
        return None
    return dt.isoformat()


def _row_to_run(row: sqlite3.Row) -> Run:
    return Run(
        id=row["id"],
        agent=row["agent"],
        status=row["status"],
        result=row["result"],
        output=row["output"],
        error=row["error"],
        started_at=_parse_iso(row["started_at"]),
        finished_at=_parse_iso(row["finished_at"]),
        duration_ms=row["duration_ms"],
    )


def _parse_iso(s: str | None) -> datetime | None:
    if s is None:
        return None
    return datetime.fromisoformat(s)
