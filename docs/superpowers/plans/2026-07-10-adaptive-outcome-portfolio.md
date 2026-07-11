# Adaptive Outcome Portfolio Implementation Plan

> **For agentic workers:** REQUIRED SUB-SKILL: Use superpowers:subagent-driven-development (recommended) or superpowers:executing-plans to implement this plan task-by-task. Steps use checkbox (`- [ ]`) syntax for tracking.

**Goal:** Turn Watchd from a run-every-schedule wrapper into a deterministic portfolio runtime that allocates finite AI spend from verified outcomes while preserving legacy behavior.

**Architecture:** Add a pure `internal/portfolio` package for policy, goals, statistics, eligibility, and scoring. Extend the existing agent, store, runner, daemon, and CLI packages through narrow interfaces. Keep JSON files and synchronous execution, with verification and human ratings as the only outcome sources.

**Tech Stack:** Go 1.25, `gopkg.in/yaml.v3`, filesystem JSON/Markdown, Claude CLI shim tests.

---

## File map

- Create `internal/portfolio/portfolio.go`: policy and goal parsing, semantic goal hashes, allocation inputs and decisions.
- Create `internal/portfolio/portfolio_test.go`: parsing, scoring, budget, review, and compatibility tests.
- Create `internal/runner/verifier.go`: bounded shell verifier with timeout and evidence classification.
- Create `internal/runner/verifier_test.go`: verifier process behavior.
- Modify `internal/agent/agent.go`: goal and verifier fields plus validation.
- Create `internal/agent/agent_test.go`: agent parsing validation.
- Modify `internal/store/store.go`: allocation, outcome, and verification records plus rating append/query helpers.
- Modify `internal/store/store_test.go`: round-trip and rating-history tests.
- Modify `internal/runner/runner.go`: authority, verification, automatic outcomes, and injectable Claude invocation.
- Modify `internal/runner/runner_test.go`: evidence and authority flow tests.
- Modify `internal/daemon/daemon.go`: adaptive sequential admission with legacy fallback.
- Create `internal/daemon/daemon_test.go`: deterministic admission and rollover tests.
- Modify `internal/cli/cli.go`: `portfolio`, `outcome`, `check`, portfolio admission, rendering, and v1.1 version.
- Create `internal/cli/cli_test.go`: command behavior through temporary working directories.
- Modify `README.md`: v1.1 portfolio setup and workflow.
- Modify `CLAUDE.md`: new invariants and test commands.
- Create `internal/cli/smoke_test.go`: end-to-end local Claude shim scenario.

### Task 1: Persist evidence and outcomes

**Files:**
- Modify: `internal/store/store.go`
- Modify: `internal/store/store_test.go`

- [ ] **Step 1: Write failing round-trip and append tests**

Add tests that save a run with `Goal`, `GoalHash`, `Allocation`, two outcome ratings, and verification evidence. Assert every field round-trips and `LatestOutcome()` returns the last rating. Add a test that `AppendOutcome` rejects `pending` and appends to a terminal run.

- [ ] **Step 2: Run the focused tests and verify failure**

Run: `go test ./internal/store -run 'TestRunEvidenceRoundTrip|TestAppendOutcome' -v`

Expected: compile failure because the new types and methods do not exist.

- [ ] **Step 3: Add the store types and helpers**

Implement the exact optional fields from the approved spec. Add:

```go
func (r *Run) LatestOutcome() *OutcomeRating
func (s *Store) AppendOutcome(id string, rating OutcomeRating) (*Run, error)
```

Reject `pending` and `approved`. Validate value and source before writing. Persist before returning success.

- [ ] **Step 4: Run store tests**

Run: `go test ./internal/store -v`

Expected: all store tests pass.

- [ ] **Step 5: Commit**

```bash
git add internal/store/store.go internal/store/store_test.go
git commit -m "feat: persist portfolio evidence"
```

### Task 2: Parse portfolio configuration and goals

**Files:**
- Create: `internal/portfolio/portfolio.go`
- Create: `internal/portfolio/portfolio_test.go`
- Modify: `internal/agent/agent.go`
- Create: `internal/agent/agent_test.go`

- [ ] **Step 1: Write failing policy, goal, and agent tests**

Cover defaults, invalid numbers, invalid authority, semantic hashes that ignore weight and budget, unknown goals, missing portfolio agent budgets, paired verifier fields, and positive verifier timeouts.

- [ ] **Step 2: Verify focused failures**

Run: `go test ./internal/portfolio ./internal/agent -v`

Expected: compile failures for missing portfolio types and agent fields.

- [ ] **Step 3: Implement parsers and validation**

Expose:

```go
func LoadPolicy(path string) (*Policy, error)
func DiscoverGoals(dir string) (map[string]*Goal, error)
func Resolve(a *agent.Agent, goals map[string]*Goal, policy *Policy) (*ResolvedAgent, error)
```

Compute the semantic goal hash from name, authority, and trimmed body. Return aggregated discovery errors instead of warnings. Add `Goal`, `Verify`, and `VerifyTimeout` to `agent.Agent` with a parsed timeout helper.

- [ ] **Step 4: Run parsing tests**

Run: `go test ./internal/portfolio ./internal/agent -v`

Expected: all tests pass.

- [ ] **Step 5: Commit**

```bash
git add internal/portfolio/portfolio.go internal/portfolio/portfolio_test.go internal/agent/agent.go internal/agent/agent_test.go
git commit -m "feat: define portfolio goals"
```

### Task 3: Implement deterministic allocation

**Files:**
- Modify: `internal/portfolio/portfolio.go`
- Modify: `internal/portfolio/portfolio_test.go`

- [ ] **Step 1: Write table-driven failing allocator tests**

Cover useful outcomes per dollar, goal weight, goal caps, global cap, exploration, harmful outcomes, unrated output, pending caps, current strategy hashes, errors as non-useful samples, exact tie breakers, and non-finite input rejection.

- [ ] **Step 2: Verify allocator tests fail**

Run: `go test ./internal/portfolio -run 'TestAllocate|TestScore' -v`

Expected: compile failure for missing allocator functions.

- [ ] **Step 3: Implement pure decisions**

Add pure functions:

```go
func BuildSnapshot(now time.Time, policy Policy, goals map[string]*Goal, agents []*ResolvedAgent, runs []store.Run) (Snapshot, error)
func Decide(snapshot Snapshot) []Decision
```

Use the approved formula and deterministic tie breakers. Every decision carries either an admission score and reservation or one explicit skip reason. Do not invoke Claude or write files.

- [ ] **Step 4: Run portfolio tests**

Run: `go test ./internal/portfolio -v`

Expected: all tests pass.

- [ ] **Step 5: Commit**

```bash
git add internal/portfolio/portfolio.go internal/portfolio/portfolio_test.go
git commit -m "feat: allocate agent budget"
```

### Task 4: Add verification and authority to execution

**Files:**
- Create: `internal/runner/verifier.go`
- Create: `internal/runner/verifier_test.go`
- Modify: `internal/runner/runner.go`
- Modify: `internal/runner/runner_test.go`

- [ ] **Step 1: Write failing verifier tests**

Test exit zero, ordinary nonzero, 126 and 127, combined output, 8 KiB truncation, timeout, and start errors. Use temporary scripts and no external model.

- [ ] **Step 2: Write failing orchestration tests**

Inject a fake Claude invoker. Test pre-satisfied skip, useful post-verification, incomplete neutral, verifier infrastructure error, observe read-only execution, propose pending execution, act execution, gate restriction, and stale approval supersession.

- [ ] **Step 3: Verify runner tests fail**

Run: `go test ./internal/runner -v`

Expected: compile failures for verification and resolved authority interfaces.

- [ ] **Step 4: Implement the verifier and runner flow**

Keep the current exported `Run` and `Approve` compatibility wrappers. Add portfolio-aware variants accepting resolved authority and allocation evidence. Make Claude invocation a package variable restored with `t.Cleanup` in tests. Update memory only after actual model output.

- [ ] **Step 5: Run runner tests**

Run: `go test ./internal/runner -v`

Expected: all tests pass.

- [ ] **Step 6: Commit**

```bash
git add internal/runner/verifier.go internal/runner/verifier_test.go internal/runner/runner.go internal/runner/runner_test.go
git commit -m "feat: verify portfolio outcomes"
```

### Task 5: Add portfolio CLI

**Files:**
- Modify: `internal/cli/cli.go`
- Create: `internal/cli/cli_test.go`

- [ ] **Step 1: Write failing CLI tests**

Test `outcome` append and validation, `check` pass and fail, `portfolio` budget and score rendering, list goal rendering, and portfolio admission for manual run and approval. Use temporary directories and injected runner functions.

- [ ] **Step 2: Verify CLI tests fail**

Run: `go test ./internal/cli -v`

Expected: compile failures or unknown command failures.

- [ ] **Step 3: Implement commands and rendering**

Add `portfolio`, `outcome`, and `check`, then apply the same admission helper to `run` and `approve`. Bump version to `1.1.0`. Preserve all legacy output when policy is absent.

- [ ] **Step 4: Run CLI tests**

Run: `go test ./internal/cli -v`

Expected: all tests pass.

- [ ] **Step 5: Commit**

```bash
git add internal/cli/cli.go internal/cli/cli_test.go
git commit -m "feat: expose portfolio outcomes"
```

### Task 6: Integrate adaptive daemon admission

**Files:**
- Modify: `internal/daemon/daemon.go`
- Create: `internal/daemon/daemon_test.go`

- [ ] **Step 1: Write failing daemon tests**

Inject clock, ticker boundary, and run function. Prove legacy run-all behavior, score order, recomputation after actual cost, budget stop, skipped agents remaining due without repeated logs, review-state resume, and local-day rollover.

- [ ] **Step 2: Verify daemon tests fail**

Run: `go test ./internal/daemon -v`

Expected: compile failures for missing injected scheduler boundary.

- [ ] **Step 3: Implement one-cycle admission**

Extract a deterministic `runDue` function used by startup and ticker paths. In portfolio mode, rebuild the snapshot after every admitted run. Keep the daemon sequential. Track emitted skip reasons in memory only to suppress duplicate console output.

- [ ] **Step 4: Run daemon tests**

Run: `go test ./internal/daemon -v`

Expected: all tests pass.

- [ ] **Step 5: Commit**

```bash
git add internal/daemon/daemon.go internal/daemon/daemon_test.go
git commit -m "feat: schedule adaptive portfolios"
```

### Task 7: Document and smoke-test the release

**Files:**
- Modify: `README.md`
- Modify: `CLAUDE.md`
- Create: `internal/cli/smoke_test.go`

- [ ] **Step 1: Write the failing smoke test**

Create a temporary portfolio with two goals and four agents plus a Claude shim. Verify repair, pre-satisfied skip, human rating, neutral strategy demotion, exploration, gated authority, and portfolio output.

- [ ] **Step 2: Run the smoke test**

Run: `go test ./internal/cli -run TestPortfolioSmoke -v`

Expected: fail until all integration paths are wired.

- [ ] **Step 3: Update user and agent documentation**

Document the one-minute setup, policy and goal fields, outcome workflow, authority, verifier safety, allocation formula in plain language, legacy behavior, and commands. Update repository invariants in `CLAUDE.md`.

- [ ] **Step 4: Run release verification**

Run:

```bash
gofmt -w cmd internal
go test ./...
go vet ./...
go build ./cmd/watchd
wc -l cmd/watchd/main.go internal/agent/*.go internal/cli/cli.go internal/daemon/daemon.go internal/portfolio/portfolio.go internal/runner/*.go internal/store/store.go
```

Expected: tests, vet, and build pass; production files remain below 2,250 lines in total after excluding `_test.go`.

- [ ] **Step 5: Commit**

```bash
git add README.md CLAUDE.md internal/cli/smoke_test.go
git commit -m "docs: explain adaptive portfolios"
```

### Task 8: Completion audit

**Files:**
- Review every changed file against `docs/superpowers/specs/2026-07-10-adaptive-outcome-portfolio-design.md`.

- [ ] **Step 1: Map every acceptance criterion to evidence**

Record the covering test, command output, or code path for each criterion. Treat missing evidence as incomplete work.

- [ ] **Step 2: Review the complete diff**

Run: `git diff master...HEAD --check && git diff --stat master...HEAD`

Inspect security boundaries, shell invocation, budget admission, status transitions, backward compatibility, and error propagation.

- [ ] **Step 3: Fix material findings test-first**

For every finding, add or tighten a failing test before the implementation fix, then rerun the focused package.

- [ ] **Step 4: Run final verification from clean state**

Run: `go clean -testcache && go test ./... && go vet ./... && go build ./cmd/watchd`

Expected: all commands exit zero.

- [ ] **Step 5: Commit the completed release**

```bash
git add <only files changed by final fixes>
git commit -m "release: v1.1.0 adaptive portfolios"
```
