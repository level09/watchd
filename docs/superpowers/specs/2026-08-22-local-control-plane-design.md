# Watchd Local Control Plane: First Vertical Slice

Date: 2026-08-22
Status: approved

## Decision

Add an internal, read-only snapshot builder and a `watchd serve` command that
starts an embedded local dashboard. The CLI and HTTP server derive state from
the same snapshot builder. Existing Markdown, YAML, and JSON files remain
authoritative.

This slice proves the single-binary product shape without adding mutations,
SQLite, MCP, authentication, a file watcher, or a general editor.

## Product contract

From a watchd directory, the user runs:

```text
watchd serve [--addr 127.0.0.1:7337]
```

Watchd binds to the address (default `127.0.0.1:7337`), prints the dashboard
URL, and serves until it receives an interrupt or termination signal. It does
not open a browser. A non-loopback address is rejected with an error; remote
access is a separate design.

The dashboard shows:

- portfolio mode or legacy mode;
- daily spend and remaining budget when portfolio mode is active;
- goals with weight, budget, authority, and current spend;
- agents with schedule, model, goal, budget, score, and eligibility reason;
- pending approval count;
- unrated result count;
- recent runs with status, cost, start time, and outcome.

The dashboard is read-only. Existing CLI commands remain the only mutation
interface in this slice.

## Architecture

```text
agents/*.md     goals/*.md     watchd.yaml     runs/*.json
      \             |              |               /
                   internal/app
                         |
              immutable Snapshot view
                    /           \
          existing CLI       local HTTP server
                                  |
                           embedded dashboard
```

`internal/app` is the only new domain boundary. It loads the current
filesystem state, applies the existing portfolio rules, and returns an
immutable snapshot. It does not cache state and does not own a background
process. It absorbs the loading logic that `loadPortfolio` and `eligibility`
in `internal/cli` perform today, so there is one loader, not two.

The HTTP server is an adapter. It must not parse agent files, calculate
eligibility, inspect the run ledger, or duplicate portfolio rules.

The browser is a projection. It must not read local files directly or infer
domain state from presentation strings.

## Snapshot API

Package `internal/app` exposes one function:

```go
func Snapshot(root string, now time.Time) (SnapshotView, error)
```

`root` is the watchd directory. `now` is passed explicitly so portfolio day
boundaries and tests are deterministic; callers pass `time.Now()`.

The package stays internal. It is not a public Go API and makes no
compatibility promise beyond the JSON shape served at `/api/v1/snapshot`. It
becomes public only when a second consumer outside this module exists.

The snapshot contains presentation-neutral values:

```go
type SnapshotView struct {
    GeneratedAt time.Time
    Mode        string
    Summary     Summary
    Goals       []GoalView
    Agents      []AgentView
    RecentRuns  []RunView
}
```

The view type is `SnapshotView` because Go does not permit a package-level type
and function to share the same name.

`Mode` is `legacy` or `portfolio`. The `/api/v1` path prefix is the contract
version; there is no separate schema field.

`Summary` contains the configured daily budget, current spend, remaining
budget, pending count and cap, and unrated count and cap. Budget and cap fields
are nullable when legacy mode has no corresponding policy.

`GoalView` contains name, description, weight, authority, optional daily
budget, current spend, useful outcome count, and optional cost per useful
outcome.

`AgentView` contains name, goal, schedule, model, authority, per-run budget,
manual-only state, optional allocation score, rated sample count, optional
estimated cost, decision reason, and optional latest run.

`RunView` contains ID, agent, goal, status, result summary, error summary, cost,
duration, start time, latest outcome, and optional allocation reason.

The view structs expose typed values, not preformatted columns, currency, age
labels, or terminal icons. Optional numbers and nested values use pointers so
JSON distinguishes an absent value from zero. Collections use deterministic
ordering. JSON field names form the first HTTP read contract.

This package uses the current `internal/agent`, `internal/portfolio`, and
`internal/store` packages and adds no rules of its own.

## Snapshot semantics

Each call reads a coherent best-effort filesystem snapshot. Existing atomic run
writes prevent partial run JSON from becoming visible. Authored files can still
change between individual reads, which matches current CLI behavior.

The application follows these rules:

1. Load agents with strict validation.
2. Load the policy if `watchd.yaml` exists.
3. In portfolio mode, load goals, resolve agents, load runs, build the existing
   portfolio snapshot, and call the existing deterministic allocator.
4. In legacy mode, load agents and runs without requiring goals or per-run
   budgets.
5. Return errors for invalid authored state, naming the offending file. Do
   not omit invalid records or return a partial successful snapshot.
6. Limit recent runs to the latest 50 entries in the application layer.

Every refresh re-reads every file under `runs/`.

```text
ponytail: full rescan per snapshot; switch to mtime filtering when runs/ passes a few thousand files
```

No new scoring, eligibility, or outcome rules are introduced.

## HTTP server

Add an internal server package used by `watchd serve`.

Endpoints:

```text
GET /                 embedded dashboard
GET /api/v1/snapshot  current Snapshot as JSON
GET /healthz          process health
```

`/healthz` reports only that the server can answer requests. It does not claim
that the current watchd files are valid. `/api/v1/snapshot` returns HTTP 500
with this shape when the application cannot build a snapshot:

```json
{"error":{"code":"snapshot_invalid","message":"..."}}
```

The server sets a read timeout, write timeout, idle timeout, and maximum header
size. It performs graceful shutdown when the command context is canceled.

The first slice has no CORS headers and no authentication. Loopback binding is
the security boundary, which is why `serve` refuses non-loopback addresses.

## Dashboard

Use embedded HTML, CSS, and a small JavaScript module. Do not add a Node.js
runtime or frontend framework in this slice.

The page requests `/api/v1/snapshot` on load and every five seconds. A refresh
replaces the current view from one complete response. The page shows the last
successful refresh time and keeps the last successful view visible if a later
request fails. It also shows the current error without claiming the data is
fresh.

The layout has four sections:

1. summary strip;
2. portfolio and goals;
3. agents and admission decisions;
4. recent activity.

The UI must remain useful at narrow desktop widths. Mobile optimization,
charts, animation, themes, and editing controls are outside this slice.

## CLI integration

Add `serve` to command dispatch and help output.

The existing status and portfolio commands consume `internal/app` and drop
their private loaders. Formatting remains inside `internal/cli`. Other
commands continue to use their current paths.

This is a targeted convergence, not a rewrite of `internal/cli/cli.go`.

## Error behavior

- Invalid configuration stops snapshot generation and is visible in the API
  and dashboard.
- A port conflict fails `watchd serve` immediately with the bind error.
- A non-loopback `--addr` fails before binding.
- Missing `agents/` follows the existing discovery error behavior.
- Corrupt run JSON fails snapshot generation and names the affected file.
- Browser refresh failures do not clear the last valid view.
- Server shutdown errors are returned unless they are the normal result of
  context cancellation.

There are no silent fallbacks from portfolio mode to legacy mode.

## Testing

### Application tests

- Build a deterministic legacy snapshot from fixture files.
- Build a deterministic portfolio snapshot and preserve existing admission
  scores and reasons.
- Sort goals, agents, and runs deterministically.
- Reject invalid policy, goal, agent, and run files and name the file.
- Apply the 50-run limit after ordering.
- Use the passed time for daily spend and generated time.

### Server tests

- Serve the embedded dashboard.
- Return a valid snapshot response and content type.
- Return a JSON error when snapshot construction fails.
- Report process health independently of authored-state validity.
- Reject unsupported methods.
- Stop cleanly when its context is canceled.

### CLI tests

- Document `serve` in help.
- Reject extra positional arguments and non-loopback addresses.
- Surface bind errors. Snapshot errors remain visible through the API and
  dashboard after the server starts.

### Release verification

```text
GOCACHE=/tmp/watchd-go-cache go test ./...
GOCACHE=/tmp/watchd-go-cache go vet ./...
GOCACHE=/tmp/watchd-go-cache go build ./cmd/watchd
```

A smoke test starts the server on an ephemeral loopback port through the server
package, requests the dashboard and snapshot, and then cancels its context. The
test does not open a browser or start a persistent process.

## Acceptance criteria

- One compiled binary contains the daemon, CLI, HTTP server, and dashboard
  assets.
- `watchd serve` exposes the current directory through a local read-only UI.
- The HTTP response and the status and portfolio commands come from the same
  snapshot builder, and `internal/cli` no longer has its own loader.
- Portfolio decisions match the existing allocator for identical state and
  time.
- No browser code reads or writes the filesystem.
- Existing commands, file formats, and stored runs remain compatible.
- No new runtime dependency is required.
- Tests, vet, and build pass.

## Deferred work

- approve, reject, rate, or edit operations in the UI;
- file hashes and compare-and-swap writes;
- filesystem watching and server-sent events;
- MCP tools;
- external-agent acquire, complete, cancel, and lease expiry;
- SQLite operational state;
- remote access, authentication, and multiple users;
- automatic browser launch;
- a general Markdown or block editor.

The next slice should add one mutation path through `internal/app`, starting
with approve, reject, and rate. It requires a separate reviewed design because
it changes authority and concurrency behavior.
