# Mission Control Implementation Plan

> **For agentic workers:** REQUIRED SUB-SKILL: Use superpowers:executing-plans to implement this plan task-by-task. Steps use checkbox (`- [ ]`) syntax for tracking.

**Goal:** Let the local dashboard run a manual agent, show its completed result, and rate the outcome.

**Architecture:** Extract CLI run and outcome orchestration into `internal/service`. Add a loopback HTTP job manager for asynchronous runs. Keep completed runs in the existing JSON ledger and make the browser poll job state.

**Tech Stack:** Go 1.25, standard library HTTP, existing runner and store packages, embedded HTML/CSS/JavaScript.

**Spec:** `docs/superpowers/specs/2026-08-30-mission-control-design.md`

## Global Constraints

- Loopback-only dashboard; preserve same-origin checks for JSON writes.
- Files remain the source of truth.
- No live token streaming, persistent job queue, edit/delete UI, or goal/policy builder in this slice.
- CLI and HTTP paths use the same service layer.
- Preserve existing tests, `go vet`, and `go build`.

---

### Task 1: Extract service orchestration

**Files:**
- Create: `internal/service/service.go`
- Create: `internal/service/service_test.go`
- Modify: `internal/cli/cli.go`

**Interfaces:**
- Produces `service.RunAgent(root, name) (*store.Run, error)` and `service.RateOutcome(root, id, value, note) (*store.Run, error)`.
- Consumes existing `agent.Load`, portfolio admission logic, `runner.Run`, `runner.RunResolved`, and `store.AppendOutcome`.

- [ ] Write a failing service test with a fake Claude executable proving `RunAgent` persists a terminal run.
- [ ] Run `GOCACHE=/tmp/watchd-go-cache go test ./internal/service`; confirm the package or symbol is missing.
- [ ] Move admission and run orchestration into the service without printing output. Move outcome append and harmful notification handling into the service.
- [ ] Refactor `cmdRun` and `cmdOutcome` to call the service and retain their existing terminal output.
- [ ] Run `GOCACHE=/tmp/watchd-go-cache go test ./internal/service ./internal/cli`; confirm green.
- [ ] Commit `feat: share run service with dashboard`.

### Task 2: Add asynchronous run jobs

**Files:**
- Create: `internal/server/jobs.go`
- Create: `internal/server/jobs_test.go`
- Modify: `internal/server/server.go`

**Interfaces:**
- `jobManager.Start(agent string) (Job, error)` queues one manual agent run.
- `jobManager.Get(id string) (Job, bool)` returns queued, running, or complete state.
- HTTP: `POST /api/v1/runs` returns `202`; `GET /api/v1/runs/{job_id}` returns job state and completed run view.

- [ ] Write tests for queueing, duplicate same-agent rejection, completion, and unknown job.
- [ ] Run the focused tests and confirm they fail before implementation.
- [ ] Implement a mutex-protected in-memory map. Use `crypto/rand` for job IDs. Execute `service.RunAgent` in a goroutine and store only job metadata.
- [ ] Add strict JSON decoding, same-origin checks, and `404` or `409` errors per spec.
- [ ] Run `GOCACHE=/tmp/watchd-go-cache go test ./internal/server`; confirm green.
- [ ] Commit `feat: run agents from local dashboard`.

### Task 3: Add outcome API

**Files:**
- Create: `internal/server/outcomes_test.go`
- Modify: `internal/server/server.go`

**Interfaces:**
- HTTP: `POST /api/v1/outcomes` accepts `{run_id,value,note}` and returns the updated `store.Run`.

- [ ] Write tests for useful, neutral, harmful, invalid value, and non-terminal run.
- [ ] Run focused tests and confirm failure.
- [ ] Decode the request with the existing JSON boundary and call `service.RateOutcome`.
- [ ] Return `400` for validation errors and `200` with the updated run on success.
- [ ] Run server tests and commit `feat: rate run outcomes over http`.

### Task 4: Complete dashboard loop

**Files:**
- Modify: `internal/server/ui/index.html`
- Modify: `internal/server/ui/app.js`
- Modify: `internal/server/ui/style.css`
- Modify: `internal/server/server_test.go`

**Interfaces:**
- Manual-only agent rows expose `Run now`.
- Result panel polls `/api/v1/runs/{job_id}` and posts ratings to `/api/v1/outcomes`.

- [ ] Add served-page hooks for run buttons, result panel, rating controls, and both endpoints; run the test to see it fail.
- [ ] Add the controls and a compact result panel. Disable buttons while requests run. Keep last successful snapshot on error.
- [ ] Add polling with a bounded interval and stop after completion or error.
- [ ] Add optional note input and refresh the snapshot after rating.
- [ ] Run `node --check internal/server/ui/app.js` and all server tests.
- [ ] Commit `feat: close dashboard run loop`.

### Task 5: Full verification and documentation

**Files:**
- Modify: `README.md`
- Modify: `internal/cli/help.txt`

- [ ] Update dashboard documentation from create-only to run, inspect, and rate.
- [ ] Run `go test ./...`, `go vet ./...`, `go build ./...`, `node --check internal/server/ui/app.js`, and `git diff --check`.
- [ ] Run a manual smoke test with a fake Claude executable: create agent, run it, inspect result, rate useful, confirm snapshot changes.
- [ ] Commit `docs: document mission control loop`.
