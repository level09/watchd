CREATE TABLE runs (
  id INTEGER PRIMARY KEY,
  agent TEXT NOT NULL,
  slot INTEGER NOT NULL, -- scheduled time in ms; one run per agent per slot
  day TEXT NOT NULL,     -- UTC date the daily cap is counted against
  status TEXT NOT NULL CHECK (status IN ('running', 'ok', 'error')),
  reserved REAL NOT NULL CHECK (reserved > 0),
  cost REAL,             -- actual USD once settled
  output TEXT,
  error TEXT,
  started_at TEXT NOT NULL DEFAULT CURRENT_TIMESTAMP,
  finished_at TEXT,
  UNIQUE (agent, slot)
);
CREATE INDEX runs_day ON runs (day);
CREATE INDEX runs_agent ON runs (agent, id);

CREATE TABLE memory (
  agent TEXT PRIMARY KEY,
  body TEXT NOT NULL,
  updated_at TEXT NOT NULL DEFAULT CURRENT_TIMESTAMP
);
