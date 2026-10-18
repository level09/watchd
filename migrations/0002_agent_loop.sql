-- Agents move from code into D1 so they can be created from the dashboard or MCP.
CREATE TABLE agents (
  name TEXT PRIMARY KEY,
  prompt TEXT NOT NULL,
  schedule TEXT,         -- five-field UTC cron; NULL runs only on demand
  budget REAL NOT NULL CHECK (budget > 0),
  model TEXT,
  tools TEXT NOT NULL DEFAULT '[]',
  max_steps INTEGER NOT NULL DEFAULT 8 CHECK (max_steps BETWEEN 1 AND 30),
  memory INTEGER NOT NULL DEFAULT 0 CHECK (memory IN (0, 1)),
  public INTEGER NOT NULL DEFAULT 0 CHECK (public IN (0, 1)),
  created_at TEXT NOT NULL DEFAULT CURRENT_TIMESTAMP,
  updated_at TEXT NOT NULL DEFAULT CURRENT_TIMESTAMP
);

INSERT INTO agents (name, prompt, schedule, budget, tools, max_steps, memory) VALUES (
  'edge-scout',
  'You watch releases for the stack behind example, example and example: Cloudflare Workers and D1, the Cloudflare agents SDK, Claude Code, Hono, example, example and Vue.

Fetch these release feeds with fetch_page:
- https://github.com/cloudflare/workers-sdk/releases.atom
- https://github.com/cloudflare/agents/releases.atom
- https://github.com/anthropics/claude-code/releases.atom
- https://github.com/honojs/hono/releases.atom
- https://github.com/example-team/example/releases.atom
- https://github.com/example/example/releases.atom
- https://github.com/vuejs/core/releases.atom

Pass every release URL you consider to seen, and only report releases it returns as new. Pick at most five changes that would let the owner delete code, cut cost, close a security gap, or build something new. For each: a bold title, one line on what changed, one line on where it applies, and the release link. Skip routine bug fixes.',
  '0 7 * * 1', 0.05, '["fetch_page","seen"]', 8, 1
);

-- Deterministic "only report what is new": the agent passes stable keys, watchd remembers them.
CREATE TABLE seen (
  agent TEXT NOT NULL,
  key TEXT NOT NULL,
  first_seen TEXT NOT NULL DEFAULT CURRENT_TIMESTAMP,
  PRIMARY KEY (agent, key)
);

-- Runs gain a 'queued' state: schedules and "Run now" enqueue, the minute tick executes.
CREATE TABLE runs_v2 (
  id INTEGER PRIMARY KEY,
  agent TEXT NOT NULL,
  slot INTEGER NOT NULL,
  day TEXT NOT NULL,
  status TEXT NOT NULL CHECK (status IN ('queued', 'running', 'ok', 'error')),
  reserved REAL NOT NULL CHECK (reserved > 0),
  cost REAL,
  output TEXT,
  error TEXT,
  started_at TEXT NOT NULL DEFAULT CURRENT_TIMESTAMP,
  finished_at TEXT,
  UNIQUE (agent, slot)
);
INSERT INTO runs_v2 SELECT * FROM runs;
DROP TABLE runs;
ALTER TABLE runs_v2 RENAME TO runs;
CREATE INDEX runs_day ON runs (day);
CREATE INDEX runs_agent ON runs (agent, id);
CREATE INDEX runs_status ON runs (status);

-- One row per model call or tool call, for the dashboard trace.
CREATE TABLE steps (
  id INTEGER PRIMARY KEY,
  run_id INTEGER NOT NULL REFERENCES runs (id) ON DELETE CASCADE,
  tool TEXT NOT NULL,    -- 'model' for model calls
  input TEXT,
  output TEXT,
  cost REAL NOT NULL DEFAULT 0,
  at TEXT NOT NULL DEFAULT CURRENT_TIMESTAMP
);
CREATE INDEX steps_run ON steps (run_id, id);
