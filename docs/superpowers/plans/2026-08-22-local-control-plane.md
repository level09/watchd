# Watchd Local Control Plane Implementation Plan

> **For agentic workers:** REQUIRED SUB-SKILL: Use superpowers:subagent-driven-development (recommended) or superpowers:executing-plans to implement this plan task-by-task. Steps use checkbox (`- [ ]`) syntax for tracking.

**Goal:** Add `watchd serve`, an embedded read-only dashboard backed by the same internal snapshot builder used by the status and portfolio commands.

**Architecture:** `internal/app` reads the existing files and converts current portfolio decisions into presentation-neutral view types. `internal/server` serves that snapshot as JSON and embeds a small HTML, CSS, and JavaScript dashboard. The CLI owns process signals, while all authored and run files remain authoritative.

**Tech Stack:** Go 1.25.6, standard library `net/http`, `embed`, HTML, CSS, vanilla JavaScript, existing YAML and filesystem packages.

**Spec:** `docs/superpowers/specs/2026-08-22-local-control-plane-design.md`

## Global Constraints

- Bind only to loopback addresses. Default to `127.0.0.1:7337`.
- Add no runtime dependency, frontend framework, Node.js runtime, SQLite, MCP, authentication, mutation, file watcher, or general editor.
- Read every file under `runs/` on each snapshot and limit recent runs to 50 after ordering.
- Keep Markdown, YAML, and JSON files authoritative.
- Reuse existing portfolio scoring and eligibility rules without changing them.
- Return errors for invalid authored state and name the offending file.
- Use no em dash or en dash in source, UI copy, documentation, commits, or release text.
- Stage only files changed by each task. Never use `git add .` or `git add -A`.

## File Map

- Create `internal/app/model.go`: JSON view types shared by CLI and HTTP.
- Create `internal/app/snapshot.go`: the only filesystem snapshot builder.
- Create `internal/app/snapshot_test.go`: legacy, portfolio, ordering, limit, and invalid-file tests.
- Create `internal/server/server.go`: listener, routes, JSON errors, and graceful shutdown.
- Create `internal/server/server_test.go`: route, method, address, bind, and cancellation tests.
- Create `internal/server/ui/index.html`: dashboard structure.
- Create `internal/server/ui/app.js`: polling, rendering, and stale-state behavior.
- Create `internal/server/ui/style.css`: responsive dashboard presentation.
- Modify `internal/cli/cli.go`: use the snapshot and add `serve`.
- Modify `internal/cli/cli_test.go`: preserve output and test `serve`.
- Modify `internal/cli/smoke_test.go`: stop using the deleted private loader.
- Modify `internal/cli/help.txt`: document `serve`.
- Modify `README.md`: document the dashboard and architecture.

---

### Task 1: Build the internal snapshot model

**Files:**
- Create: `internal/app/model.go`
- Create: `internal/app/snapshot.go`
- Create: `internal/app/snapshot_test.go`

**Interfaces:**
- Consumes: existing `internal/agent`, `internal/portfolio`, and `internal/store` APIs.
- Produces: `func Snapshot(root string, now time.Time) (SnapshotView, error)` and JSON view types.

- [ ] **Step 1: Write failing legacy and invalid-file tests**

Create fixture helpers and these core cases in `internal/app/snapshot_test.go`:

```go
func TestSnapshotLegacy(t *testing.T) {
    root := t.TempDir()
    now := time.Date(2026, 8, 22, 12, 0, 0, 0, time.UTC)
    writeFixture(t, root, "agents/manual.md", "---\nname: manual\nmodel: haiku\n---\nInspect on request.")
    writeFixture(t, root, "agents/scheduled.md", "---\nname: scheduled\nschedule: 1h\nbudget: 0.10\ngate: true\n---\nInspect hourly.")
    saveFixtureRun(t, root, store.Run{
        Agent: "scheduled", Status: "success", CostUSD: 0.04,
        StartedAt: now.Add(-time.Hour),
        OutcomeRatings: []store.OutcomeRating{{Value: "useful", Source: "human", RatedAt: now}},
    })

    got, err := Snapshot(root, now)
    if err != nil { t.Fatal(err) }
    if got.Mode != "legacy" || !got.GeneratedAt.Equal(now) { t.Fatalf("snapshot = %+v", got) }
    if got.Summary.DailyBudgetUSD != nil { t.Fatalf("summary = %+v", got.Summary) }
    if len(got.Agents) != 2 || got.Agents[0].Name != "manual" || !got.Agents[0].ManualOnly {
        t.Fatalf("agents = %+v", got.Agents)
    }
    if got.Agents[1].Authority != "propose" || got.Agents[1].LatestRun == nil {
        t.Fatalf("scheduled = %+v", got.Agents[1])
    }
}

func TestSnapshotNamesInvalidAgentFile(t *testing.T) {
    root := t.TempDir()
    writeFixture(t, root, "agents/broken.md", "not frontmatter")
    _, err := Snapshot(root, time.Now())
    if err == nil || !strings.Contains(err.Error(), "agents/broken.md") {
        t.Fatalf("error = %v", err)
    }
}
```

- [ ] **Step 2: Run the focused test and confirm the missing package failure**

Run:

```bash
GOCACHE=/tmp/watchd-go-cache go test ./internal/app -run 'TestSnapshot(Legacy|NamesInvalidAgentFile)$'
```

Expected: FAIL because `internal/app` does not exist.

- [ ] **Step 3: Define the exact view model**

Create `internal/app/model.go`:

```go
package app

import "time"

type SnapshotView struct {
    GeneratedAt time.Time   `json:"generated_at"`
    Mode        string      `json:"mode"`
    Summary     Summary     `json:"summary"`
    Goals       []GoalView  `json:"goals"`
    Agents      []AgentView `json:"agents"`
    RecentRuns  []RunView   `json:"recent_runs"`
}

type Summary struct {
    DailyBudgetUSD *float64 `json:"daily_budget_usd"`
    SpentUSD       float64  `json:"spent_usd"`
    RemainingUSD   *float64 `json:"remaining_usd"`
    Pending        int      `json:"pending"`
    MaxPending     *int     `json:"max_pending"`
    Unrated        int      `json:"unrated"`
    MaxUnrated     *int     `json:"max_unrated"`
}

type GoalView struct {
    Name             string   `json:"name"`
    Description      string   `json:"description"`
    Weight           float64  `json:"weight"`
    Authority        string   `json:"authority"`
    DailyBudgetUSD   *float64 `json:"daily_budget_usd"`
    SpentUSD         float64  `json:"spent_usd"`
    UsefulOutcomes   int      `json:"useful_outcomes"`
    CostPerUsefulUSD *float64 `json:"cost_per_useful_usd"`
}

type AgentView struct {
    Name             string   `json:"name"`
    Goal             string   `json:"goal,omitempty"`
    Schedule         string   `json:"schedule,omitempty"`
    Model            string   `json:"model"`
    Authority        string   `json:"authority"`
    BudgetUSD        float64  `json:"budget_usd"`
    ManualOnly       bool     `json:"manual_only"`
    Score            *float64 `json:"score"`
    RatedSamples     int      `json:"rated_samples"`
    EstimatedCostUSD *float64 `json:"estimated_cost_usd"`
    Decision         string   `json:"decision,omitempty"`
    LatestRun        *RunView `json:"latest_run,omitempty"`
}

type RunView struct {
    ID               string        `json:"id"`
    Agent            string        `json:"agent"`
    Goal             string        `json:"goal,omitempty"`
    Status           string        `json:"status"`
    Result           string        `json:"result,omitempty"`
    Error            string        `json:"error,omitempty"`
    CostUSD          float64       `json:"cost_usd"`
    DurationMS       int64         `json:"duration_ms"`
    StartedAt        time.Time     `json:"started_at"`
    Outcome          string        `json:"outcome,omitempty"`
    AllocationReason string        `json:"allocation_reason,omitempty"`
}
```

Use `SnapshotView` because Go cannot declare a type and function named `Snapshot` in one package.

- [ ] **Step 4: Implement strict loading and legacy conversion**

Create `internal/app/snapshot.go` with this entry point:

```go
func Snapshot(root string, now time.Time) (SnapshotView, error) {
    agentsPath := filepath.Join(root, "agents")
    agents, err := agent.DiscoverStrict(agentsPath)
    if err != nil { return SnapshotView{}, fmt.Errorf("load %s: %w", agentsPath, err) }
    sort.Slice(agents, func(i, j int) bool { return agents[i].Name < agents[j].Name })

    policyPath := filepath.Join(root, "watchd.yaml")
    policy, err := portfolio.LoadPolicy(policyPath)
    if err != nil { return SnapshotView{}, fmt.Errorf("load %s: %w", policyPath, err) }
    runs, err := store.New(root).GetRuns("", 0)
    if err != nil { return SnapshotView{}, err }

    if policy == nil { return legacySnapshot(now, agents, runs), nil }
    return portfolioSnapshot(root, now, *policy, agents, runs)
}
```

Implement `legacySnapshot`, `latestRuns`, `recentRunViews`, `runView`, and `runSummary`. Use these exact rules:

- legacy authority is `propose` when `gate` is true, otherwise `act`;
- all slices are non-nil and sorted by name or newest run;
- `runView` keeps 500 trimmed result bytes and 200 trimmed error bytes, and
  converts `run.Duration` with `run.Duration.Milliseconds()`;
- recent runs are copied after `store.GetRuns` sorting and capped at 50;
- daily spend uses the local calendar day of the passed `now`;
- pending counts status `pending`; unrated counts status `success` with no latest outcome.

- [ ] **Step 5: Add ordering, limit, and corrupt-run tests**

Add 55 runs with descending timestamps and assert that `RecentRuns` has 50 entries with the newest first. Write `runs/corrupt.json` containing `{` and assert that the returned error contains `corrupt.json`.

```go
if len(got.RecentRuns) != 50 || !got.RecentRuns[0].StartedAt.Equal(now) {
    t.Fatalf("runs = %+v", got.RecentRuns)
}
```

- [ ] **Step 6: Run, format, and commit the snapshot model**

Run:

```bash
gofmt -w internal/app/model.go internal/app/snapshot.go internal/app/snapshot_test.go
GOCACHE=/tmp/watchd-go-cache go test ./internal/app
git add internal/app/model.go internal/app/snapshot.go internal/app/snapshot_test.go
git commit -m "feat: add control plane snapshot"
```

### Task 2: Preserve portfolio semantics and converge CLI reads

**Files:**
- Modify: `internal/app/snapshot.go`
- Modify: `internal/app/snapshot_test.go`
- Modify: `internal/cli/cli.go`
- Modify: `internal/cli/cli_test.go`
- Modify: `internal/cli/smoke_test.go`

**Interfaces:**
- Consumes: `app.Snapshot(root, now) (app.SnapshotView, error)` from Task 1.
- Produces: complete portfolio views; status, list, and portfolio output with no private `loadPortfolio` or `eligibility` loader.

- [ ] **Step 1: Write a failing portfolio parity test**

Create a policy with one goal, one scheduled agent, one manual goal agent, and one useful historical run. Load the scheduled agent and goal hashes through their existing packages before saving the run. Assert:

```go
got, err := Snapshot(root, now)
if err != nil { t.Fatal(err) }
if got.Mode != "portfolio" || *got.Summary.RemainingUSD != 0.95 {
    t.Fatalf("summary = %+v", got.Summary)
}
if len(got.Goals) != 1 || got.Goals[0].UsefulOutcomes != 1 || *got.Goals[0].CostPerUsefulUSD != 0.05 {
    t.Fatalf("goals = %+v", got.Goals)
}
if got.Agents[0].Name != "alpha" || got.Agents[0].Decision != "highest verified return" || got.Agents[0].RatedSamples != 1 {
    t.Fatalf("alpha = %+v", got.Agents[0])
}
if got.Agents[1].Name != "manual" || got.Agents[1].Decision != "manual only" {
    t.Fatalf("manual = %+v", got.Agents[1])
}
```

- [ ] **Step 2: Run the portfolio test and confirm the missing conversion failure**

Run:

```bash
GOCACHE=/tmp/watchd-go-cache go test ./internal/app -run TestSnapshotPortfolioPreservesAllocation
```

Expected: FAIL because `portfolioSnapshot` is not implemented.

- [ ] **Step 3: Implement portfolio conversion without new rules**

Use existing resolution and allocation functions:

```go
func portfolioSnapshot(root string, now time.Time, policy portfolio.Policy, agents []*agent.Agent, runs []store.Run) (SnapshotView, error) {
    goalsPath := filepath.Join(root, "goals")
    goals, err := portfolio.DiscoverGoals(goalsPath)
    if err != nil { return SnapshotView{}, fmt.Errorf("load %s: %w", goalsPath, err) }

    resolvedByName := make(map[string]*portfolio.ResolvedAgent)
    var scheduled []*portfolio.ResolvedAgent
    for _, item := range agents {
        if item.Goal == "" {
            if item.Schedule != "" { return SnapshotView{}, fmt.Errorf("agent %q has no goal in portfolio mode", item.Name) }
            continue
        }
        resolved, err := portfolio.Resolve(item, goals, &policy)
        if err != nil { return SnapshotView{}, fmt.Errorf("resolve %s: %w", item.FilePath, err) }
        resolvedByName[item.Name] = resolved
        if item.Schedule != "" { scheduled = append(scheduled, resolved) }
    }
    state, err := portfolio.BuildSnapshot(now, policy, goals, scheduled, runs)
    if err != nil { return SnapshotView{}, err }
    decisions := portfolio.Decide(state)
    return buildPortfolioView(now, policy, goals, agents, runs, state, resolvedByName, decisions), nil
}
```

`buildPortfolioView` must:

- sort goals and agents by name;
- copy budget, spend, remaining budget, pending, unrated, and caps;
- count useful outcomes only for non-satisfied runs whose latest outcome is useful;
- divide total goal cost by useful count to preserve current efficiency output;
- copy resolved authority, decision score, estimated cost, reason, and `portfolio.StatsFor(...).Rated`;
- label every unscheduled agent `manual only`;
- keep unlinked manual agents visible with legacy authority and no score.

- [ ] **Step 4: Refactor status, list, and portfolio to use the snapshot**

Import `internal/app`. Each read command starts with:

```go
view, err := app.Snapshot(".", time.Now())
if err != nil { return err }
```

Preserve existing output strings and formatting. Delete `loadPortfolio` and `eligibility`. Do not change `admitAgent`, daemon loading, mutation commands, or runner behavior. Replace the final `loadPortfolio` block in `internal/cli/smoke_test.go` with a map built from `view.Agents`.

- [ ] **Step 5: Run parity tests**

Run:

```bash
GOCACHE=/tmp/watchd-go-cache go test ./internal/app ./internal/cli
```

Expected: PASS, including current assertions for `remaining $0.9500`, `1 useful`, `$0.0500/useful`, `1 rated`, and `highest verified return`.

- [ ] **Step 6: Format and commit CLI convergence**

Run:

```bash
gofmt -w internal/app/snapshot.go internal/app/snapshot_test.go internal/cli/cli.go internal/cli/cli_test.go internal/cli/smoke_test.go
git add internal/app/snapshot.go internal/app/snapshot_test.go internal/cli/cli.go internal/cli/cli_test.go internal/cli/smoke_test.go
git commit -m "refactor: unify control plane reads"
```

### Task 3: Add the loopback HTTP server

**Files:**
- Create: `internal/server/server.go`
- Create: `internal/server/server_test.go`
- Create: `internal/server/ui/index.html`
- Create: `internal/server/ui/app.js`
- Create: `internal/server/ui/style.css`

**Interfaces:**
- Consumes: `app.Snapshot(root, now)`.
- Produces: `server.New(root, addr string, now func() time.Time) (*Server, error)`, `(*Server).Addr() net.Addr`, and `(*Server).Serve(ctx context.Context) error`.

- [ ] **Step 1: Write failing route, address, and cancellation tests**

Use `httptest` against an unexported `newHandler`. Verify `GET /`, `GET /app.js`, `GET /style.css`, `GET /api/v1/snapshot`, and `GET /healthz`. Verify POST to a GET route returns 405. For invalid authored state, assert:

```go
if response.Code != http.StatusInternalServerError ||
    !strings.Contains(response.Body.String(), `"code":"snapshot_invalid"`) {
    t.Fatalf("status=%d body=%s", response.Code, response.Body.String())
}
```

Verify health remains 200 for the same invalid root. Verify `New` rejects `0.0.0.0:7337`, `192.0.2.1:7337`, and `:7337`. Start a server on `127.0.0.1:0`, cancel its context, and assert `Serve` returns nil.

- [ ] **Step 2: Run server tests and confirm the missing package failure**

Run:

```bash
GOCACHE=/tmp/watchd-go-cache go test ./internal/server
```

Expected: FAIL because `internal/server` does not exist.

- [ ] **Step 3: Implement the server boundary**

Create the server with listener ownership so bind errors happen before the blocking serve call:

```go
type Server struct {
    http     *http.Server
    listener net.Listener
}

func New(root, addr string, now func() time.Time) (*Server, error) {
    if err := validateLoopback(addr); err != nil { return nil, err }
    listener, err := net.Listen("tcp", addr)
    if err != nil { return nil, fmt.Errorf("listen on %s: %w", addr, err) }
    return &Server{
        listener: listener,
        http: &http.Server{
            Handler: newHandler(root, now),
            ReadHeaderTimeout: 5 * time.Second,
            ReadTimeout: 10 * time.Second,
            WriteTimeout: 10 * time.Second,
            IdleTimeout: 30 * time.Second,
            MaxHeaderBytes: 1 << 20,
        },
    }, nil
}

func (s *Server) Addr() net.Addr { return s.listener.Addr() }
```

`validateLoopback` uses `net.SplitHostPort`. Accept only `localhost`, an IP in `127.0.0.0/8`, or `::1`. Reject an empty host, any other hostname, and any non-loopback IP before binding.

Embed `ui/*`. Register method-specific routes for `/`, `/app.js`, `/style.css`, `/api/v1/snapshot`, and `/healthz`. Unknown paths return 404. Set JSON content type before encoding. Use this error shape:

```go
type errorEnvelope struct {
    Error struct {
        Code    string `json:"code"`
        Message string `json:"message"`
    } `json:"error"`
}
```

On context cancellation, call `http.Server.Shutdown` with a five-second timeout. Return nil for `http.ErrServerClosed` and return other serve or shutdown errors.

- [ ] **Step 4: Add minimal embedded assets**

Use a valid HTML document that links `/style.css`, contains `<main id="app">Loading watchd...</main>`, and loads `/app.js` as a module. Use `document.querySelector('#app').textContent = 'watchd';` in the temporary JavaScript and `body { font-family: sans-serif; }` in the temporary CSS.

- [ ] **Step 5: Run, format, and commit the server**

Run:

```bash
gofmt -w internal/server/server.go internal/server/server_test.go
GOCACHE=/tmp/watchd-go-cache go test ./internal/server
git add internal/server/server.go internal/server/server_test.go internal/server/ui/index.html internal/server/ui/app.js internal/server/ui/style.css
git commit -m "feat: add local dashboard server"
```

### Task 4: Build the embedded dashboard projection

**Files:**
- Modify: `internal/server/ui/index.html`
- Modify: `internal/server/ui/app.js`
- Modify: `internal/server/ui/style.css`
- Modify: `internal/server/server_test.go`

**Interfaces:**
- Consumes: `/api/v1/snapshot` JSON from `internal/app/model.go`.
- Produces: a read-only four-section dashboard that polls every five seconds and preserves its last successful view after an error.

- [ ] **Step 1: Write a failing asset contract test**

Read the embedded files from `uiFiles`. Assert `index.html` contains `summary`, `goals`, `agents`, `activity`, `freshness`, and `error` IDs. Assert `app.js` contains `/api/v1/snapshot`, `5000`, `renderSnapshot`, and `showError`.

```go
for _, id := range []string{"summary", "goals", "agents", "activity", "freshness", "error"} {
    if !strings.Contains(page, `id="`+id+`"`) { t.Fatalf("dashboard missing %q", id) }
}
```

- [ ] **Step 2: Run the contract test and confirm it fails**

Run:

```bash
GOCACHE=/tmp/watchd-go-cache go test ./internal/server -run TestDashboardContainsRequiredProjectionHooks
```

Expected: FAIL because the minimal page has no dashboard sections.

- [ ] **Step 3: Replace the page with the final semantic structure**

The body contains no mutation controls:

```html
<body>
  <header class="masthead">
    <div><span class="mark">watchd</span><span id="mode" class="mode"></span></div>
    <div><span id="error" role="alert"></span><span id="freshness">Loading...</span></div>
  </header>
  <main>
    <section id="summary" class="summary" aria-label="Portfolio summary"></section>
    <section class="panel"><h1>Goals</h1><div id="goals" class="cards"></div></section>
    <section class="panel">
      <h1>Agents</h1>
      <div class="table-wrap"><table><thead><tr><th>Agent</th><th>Goal</th><th>Schedule</th><th>Authority</th><th>Budget</th><th>Score</th><th>Decision</th><th>Last run</th></tr></thead><tbody id="agents"></tbody></table></div>
    </section>
    <section class="panel"><h1>Recent activity</h1><div id="activity" class="activity"></div></section>
  </main>
  <script type="module" src="/app.js"></script>
</body>
```

- [ ] **Step 4: Implement safe polling and stale-state behavior**

Use DOM constructors and `textContent` for every server value. Do not place server values through `innerHTML`.

```js
const refreshInterval = 5000;
const elements = Object.fromEntries(
  ['mode', 'error', 'freshness', 'summary', 'goals', 'agents', 'activity']
    .map((id) => [id, document.getElementById(id)]),
);

function money(value) { return value == null ? '-' : `$${Number(value).toFixed(4)}`; }
function node(tag, className, text) {
  const value = document.createElement(tag);
  if (className) value.className = className;
  if (text != null) value.textContent = text;
  return value;
}

function renderSnapshot(snapshot) {
  elements.mode.textContent = snapshot.mode;
  renderSummary(snapshot.summary);
  renderGoals(snapshot.goals);
  renderAgents(snapshot.agents);
  renderActivity(snapshot.recent_runs);
  elements.error.textContent = '';
  elements.freshness.textContent = `Updated ${new Date(snapshot.generated_at).toLocaleTimeString()}`;
}

function showError(message) {
  elements.error.textContent = message;
  elements.freshness.textContent = 'Showing last successful refresh';
}

async function refresh() {
  try {
    const response = await fetch('/api/v1/snapshot', { headers: { Accept: 'application/json' } });
    const body = await response.json();
    if (!response.ok) throw new Error(body.error?.message || `HTTP ${response.status}`);
    renderSnapshot(body);
  } catch (error) {
    showError(error instanceof Error ? error.message : String(error));
  }
}

refresh();
setInterval(refresh, refreshInterval);
```

Implement the four render functions by calling `replaceChildren()` only on their own container and appending nodes. Show explicit empty messages for no goals, agents, or runs. Format age as seconds, minutes, hours, or days. Sanitize run status before using it in a CSS class by removing characters outside `[a-z0-9-]`.

- [ ] **Step 5: Implement the compact responsive CSS**

Use only local system fonts. Set content width to `min(1440px, calc(100% - 32px))`. Use four summary columns above 900px, two from 560px through 899px, and one below 560px. Keep the agent table in `.table-wrap { overflow-x: auto; }`. Use tabular numbers for money, scores, and counts. Use warm near-white, white panels, charcoal text, gray metadata, green useful states, amber pending states, and red error states. Add no animation.

- [ ] **Step 6: Run tests and commit the dashboard**

Run:

```bash
GOCACHE=/tmp/watchd-go-cache go test ./internal/server
git add internal/server/ui/index.html internal/server/ui/app.js internal/server/ui/style.css internal/server/server_test.go
git commit -m "feat: add control plane dashboard"
```

### Task 5: Expose `watchd serve` and verify the release

**Files:**
- Modify: `internal/cli/cli.go`
- Modify: `internal/cli/cli_test.go`
- Modify: `internal/cli/help.txt`
- Modify: `README.md`

**Interfaces:**
- Consumes: `server.New`, `Server.Addr`, and `Server.Serve` from Task 3.
- Produces: `watchd serve [--addr 127.0.0.1:7337]`.

- [ ] **Step 1: Write failing CLI validation, bind, and help tests**

Add `TestServeRejectsInvalidArguments` for `serve extra`, incomplete `--addr`, unknown `--port`, and `--addr 0.0.0.0:7337`. Hold an ephemeral listener and assert a second `serve` call returns an error containing `listen`. Assert help contains `serve [--addr`.

```go
listener, err := net.Listen("tcp", "127.0.0.1:0")
if err != nil { t.Fatal(err) }
defer listener.Close()
err = Run([]string{"serve", "--addr", listener.Addr().String()})
if err == nil || !strings.Contains(err.Error(), "listen") { t.Fatalf("error = %v", err) }
```

- [ ] **Step 2: Run focused CLI tests and confirm the unknown command failure**

Run:

```bash
GOCACHE=/tmp/watchd-go-cache go test ./internal/cli -run 'Test(Serve|HelpDocumentsServe)'
```

Expected: FAIL because `serve` is not a command.

- [ ] **Step 3: Add exact argument parsing and lifecycle**

Dispatch `serve` with `args[1:]`, then implement:

```go
func cmdServe(args []string) error {
    addr := "127.0.0.1:7337"
    switch {
    case len(args) == 0:
    case len(args) == 2 && args[0] == "--addr":
        addr = args[1]
    default:
        return fmt.Errorf("usage: watchd serve [--addr 127.0.0.1:7337]")
    }
    instance, err := server.New(".", addr, time.Now)
    if err != nil { return err }
    ctx, stop := signal.NotifyContext(context.Background(), syscall.SIGINT, syscall.SIGTERM)
    defer stop()
    fmt.Printf("watchd: dashboard http://%s\n", instance.Addr())
    return instance.Serve(ctx)
}
```

Add this help entry:

```text
  serve [--addr ADDRESS]
                start local read-only dashboard (default 127.0.0.1:7337)
```

- [ ] **Step 4: Run and format focused CLI tests**

Run:

```bash
gofmt -w internal/cli/cli.go internal/cli/cli_test.go
GOCACHE=/tmp/watchd-go-cache go test ./internal/cli -run 'Test(Serve|HelpDocumentsServe)'
```

Expected: PASS.

- [ ] **Step 5: Document the dashboard**

Add `watchd serve` to the README command table. Add a local dashboard section that states the URL, loopback-only boundary, read-only behavior, and filesystem source of truth. Add `internal/app` and `internal/server` to the architecture tree.

```markdown
## Local dashboard

Run `watchd serve`, then open `http://127.0.0.1:7337`. The read-only dashboard
shows goals, agents, allocation decisions, review debt, spend, and recent
outcomes. It binds only to the local machine. Existing files remain the source
of truth.
```

- [ ] **Step 6: Run complete release verification**

Run:

```bash
GOCACHE=/tmp/watchd-go-cache go test ./...
GOCACHE=/tmp/watchd-go-cache go vet ./...
GOCACHE=/tmp/watchd-go-cache go build ./cmd/watchd
```

Expected: all commands exit zero.

- [ ] **Step 7: Check final scope and commit**

Run:

```bash
git diff --check
rg -n $'\u2013|\u2014' internal/app internal/server internal/cli README.md
git status --short
git add internal/cli/cli.go internal/cli/cli_test.go internal/cli/help.txt README.md
git commit -m "feat: expose local control plane"
```

Expected: the punctuation scan produces no output. Status before staging lists only plan files plus unrelated pre-existing untracked files.

## Completion Gate

Before reporting completion:

1. Run the three release verification commands from Task 5 against the final commit.
2. Start `./watchd serve --addr 127.0.0.1:7337` from a temporary fixture directory.
3. Request `/`, `/api/v1/snapshot`, and `/healthz` with `curl` and confirm HTTP 200.
4. Stop the process with SIGTERM and confirm a zero exit status.
5. Confirm the UI has no mutation controls and the process created no authored or run files.
