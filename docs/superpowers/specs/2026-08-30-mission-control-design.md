# Mission Control: run, result, rate

## Goal

Make the local dashboard useful after an agent is created. A user should be
able to run a manual agent, inspect its result and cost, and rate the outcome
without switching to the terminal.

## Scope

This slice adds three dashboard actions:

1. `Run now` starts a manual agent run and returns without holding the browser
   request open.
2. A run detail view shows running, terminal, pending, error, result, cost,
   verification, and session state from the existing JSON run ledger.
3. `Useful`, `Neutral`, and `Harmful` actions append a human outcome rating to
   a terminal run, with an optional note.

The dashboard remains loopback-only. Files remain the source of truth. The CLI
continues to use the same service layer as the HTTP API.

## Non-goals

- Live token streaming or terminal emulation.
- Editing or deleting existing agent files.
- Creating goals or policy files in this slice.
- Authentication beyond loopback binding and same-origin write checks.
- A persistent job queue separate from the run ledger.

## Architecture

Extract the current `cmdRun` and `cmdOutcome` orchestration into a small
internal service package. The service owns agent loading, portfolio admission,
runner invocation, run persistence, and human outcome append. CLI commands and
HTTP handlers call it. The service must not write human-facing output.

The server owns an in-memory job map keyed by a random job ID. A run request
creates a job, starts the service in a goroutine, and returns `202 Accepted`.
The job stores status, run ID, and error only. The completed `store.Run` remains
the durable record. A process restart loses only an in-flight job indicator,
not a completed run.

## HTTP contract

`POST /api/v1/runs`

```json
{"agent":"repo-health"}
```

Returns `202`:

```json
{"job_id":"...","agent":"repo-health","status":"queued"}
```

`GET /api/v1/runs/{job_id}` returns `queued`, `running`, or `complete` and,
when available, the run ID and the corresponding run view. Unknown jobs return
`404`.

`POST /api/v1/outcomes`

```json
{"run_id":"...","value":"useful","note":"..."}
```

Returns the updated run. It delegates validation to the store and rejects
non-terminal runs.

All write endpoints require JSON. An `Origin` header, when present, must match
the local dashboard origin. Agent callers may omit `Origin` and use the same
JSON contract.

## Browser flow

The agent table gets a `Run now` action only for manual agents. After submit,
the row shows a queued state and polls the job endpoint. Completion opens a
result panel with the run output, cost, duration, verification, and a compact
rating control. Rating updates the run and refreshes the portfolio snapshot.

Errors remain in the panel and do not clear the last successful dashboard
snapshot. Buttons disable during their request and re-enable after failure.

## Error handling

- Unknown agent: `404`.
- Invalid or ineligible agent: `400` with the existing CLI error text.
- Already-running duplicate request for the same agent: `409`.
- Unknown job or run: `404`.
- Invalid outcome or non-terminal run: `400`.
- Cross-origin or non-JSON write: `403` or `415`.

The job manager must prevent concurrent runs of the same agent in one server
process. It must not prevent different agents from running concurrently.

## Verification

- Service tests prove CLI and HTTP paths persist equivalent runs.
- Server tests prove `202` queueing, job polling, unknown IDs, duplicate-run
  rejection, and outcome append.
- Existing Go tests, `go vet`, and `go build` remain green.
- A manual smoke test creates a manual agent, runs it with a fake Claude
  executable, rates the result, and confirms the dashboard snapshot changes.
