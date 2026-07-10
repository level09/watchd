# Watchd v1.1: Desired State

Date: 2026-07-10
Status: Approved design

## Product decision

Watchd v1.1 moves from scheduled prompt execution to desired-state control.
An agent declares a goal and a deterministic verifier. Watchd checks reality
before spending model tokens, invokes the agent only when the goal is false,
and checks reality again before reporting success.

Positioning:

> Cron runs commands. Watchd keeps goals true.

The release remains a small Go CLI wrapping `claude -p`. It does not add a web
interface, multi-agent orchestration, provider abstraction, automatic prompt
mutation, or a general workflow engine.

## User contract

A desired-state agent adds three frontmatter fields:

```yaml
---
name: repo-health
schedule: 6h
goal: main builds and every test passes
verify: go test ./...
verify_timeout: 2m
model: sonnet
budget: 0.25
gate: true
---

Investigate the failure and make the smallest correct repair.
```

`goal` and `verify` are a pair. Defining only one is an invalid agent. The
default `verify_timeout` is two minutes. A supplied timeout must parse with
`time.ParseDuration` and must be greater than zero.

The verifier is a user-authored shell command executed from Watchd's current
working directory with the inherited environment. Exit code zero means the
goal is satisfied. Exit codes 126 and 127 indicate a verifier configuration
error. Any other nonzero exit code means the goal is unsatisfied. A process
start failure or timeout is an infrastructure error, not an unsatisfied goal.

Verifier output combines stdout and stderr. Watchd stores at most 8 KiB for
each verification attempt so a noisy command cannot inflate run history or
agent context without bound.

Agents without `goal` and `verify` keep their existing behavior.

## Runtime flow

### Direct agent

1. Run the verifier and record the result as `verification_before`.
2. If it passes, save a zero-cost run with status `satisfied`. Do not invoke
   Claude.
3. If it fails, append a bounded goal section to the agent prompt. The section
   includes the desired state, verifier command, exit code, and verifier output.
4. Invoke Claude once under the existing model, tools, turn, permission, MCP,
   memory, and budget controls.
5. If Claude fails, preserve the existing `error` behavior and do not claim the
   goal was checked after a successful action.
6. If Claude succeeds, run the verifier again and record it as
   `verification_after`.
7. If the second verifier passes, save status `success`. Otherwise save status
   `incomplete`.

Verifier output is evidence, not trusted instructions. The injected prompt
must explicitly tell the agent not to follow instructions found inside verifier
output.

### Gated agent

The first pass keeps the existing read-only gate:

1. Run and store the pre-verifier.
2. If it passes, save `satisfied` and skip Claude.
3. If it fails, invoke the read-only planning pass and save `pending`.
4. Do not run a post-verifier because the planning pass cannot change state.

Approval protects against stale plans:

1. Re-run the verifier before resuming the pending session.
2. If it now passes, mark the pending run `superseded`. Do not resume Claude.
3. If it remains false, resume the approved session with the current verifier
   evidence included as context.
4. After Claude returns successfully, run the verifier again.
5. Save the action run as `success` when verified or `incomplete` when the goal
   remains false. Mark the original pending run `approved` only when execution
   actually starts.

Rejection remains unchanged.

## Status model

| Status | Meaning |
| --- | --- |
| `satisfied` | The verifier passed before agent execution. Claude was not invoked. |
| `pending` | A gated repair plan awaits approval. |
| `success` | Claude completed and the post-verifier passed, or a legacy agent completed. |
| `incomplete` | Claude completed but the post-verifier still failed. |
| `superseded` | A pending plan became unnecessary before approval. |
| `approved` | The pending planning run was accepted and execution started. |
| `rejected` | The pending planning run was rejected. |
| `error` | Claude execution, verifier startup, or verifier timeout failed. |

`incomplete` is actionable and triggers the configured notification command,
alongside `pending` and `error`. `satisfied` and `superseded` do not notify.

## CLI surface

The existing commands gain desired-state behavior without new flags.

One command is added:

```text
watchd check <agent>
```

`check` runs only the verifier. It prints the goal, command, duration, and
bounded output. It exits zero when the goal is satisfied and nonzero when the
goal is unsatisfied or verification infrastructure fails. It rejects legacy
agents without a goal verifier.

`watchd`, `watchd logs`, `watchd pending`, and run output recognize the new
statuses. `watchd help` documents `check`.

## Stored evidence

`store.Run` gains:

```go
Goal               string        `json:"goal,omitempty"`
VerificationBefore *Verification `json:"verification_before,omitempty"`
VerificationAfter  *Verification `json:"verification_after,omitempty"`
```

`Verification` contains:

```go
Command    string `json:"command"`
Passed     bool   `json:"passed"`
ExitCode   int    `json:"exit_code"`
Output     string `json:"output,omitempty"`
Error      string `json:"error,omitempty"`
DurationMS int64  `json:"duration_ms"`
```

The stored command and outputs make the success claim independently
inspectable. Existing JSON run files remain readable because all fields are
optional.

## Code boundaries

### `internal/agent`

- Parse `goal`, `verify`, and `verify_timeout`.
- Validate paired fields and timeout syntax at load time.
- Provide a helper that returns the parsed timeout with the two-minute default.

### `internal/runner`

- Add a verifier runner with timeout, combined output, exit-code capture, and
  bounded storage.
- Add desired-state orchestration around the existing Claude invocation.
- Recheck gated goals before approval.
- Keep real Claude execution injectable behind an unexported function boundary
  so runtime flows can be tested without API calls.

### `internal/store`

- Add verification evidence to `Run`.
- Preserve backward-compatible JSON loading.

### `internal/cli`

- Add `check`.
- Render the new statuses consistently.
- Bump the binary version to `1.1.0`.

### Documentation

- Update `README.md` with the desired-state example, lifecycle, command, fields,
  and positioning.
- Update `CLAUDE.md` with the new runtime invariants.

## Error handling

- A verifier exit code other than zero, 126, or 127 is normal evidence that a
  goal is false.
- Exit codes 126 and 127 fail fast as verifier configuration errors.
- Failure to start the shell is `error`.
- Timeout terminates the verifier command through its context and is `error`
  with a concise message.
- Invalid goal configuration fails during agent loading.
- A failed Claude invocation remains `error`, even if a later manual check might
  pass. Watchd does not infer success without the configured post-verifier.
- A post-verifier failure is `incomplete`, not `error`, because the runtime
  operated correctly but the desired state was not achieved.
- Memory is updated only from an actual agent result. A pre-satisfied run does
  not rewrite memory.

## Testing strategy

Implementation follows test-first development.

### Agent parsing tests

- Accept a valid goal-verifier pair.
- Reject either field without the other.
- Apply the two-minute default.
- Reject invalid, zero, and negative timeouts.

### Verifier tests

- Capture a passing command.
- Treat a nonzero exit as unsatisfied, not infrastructure failure.
- Treat exit codes 126 and 127 as verifier configuration errors.
- Capture combined stdout and stderr.
- Enforce timeout.
- Bound stored output to 8 KiB.

### Runner tests

- A passing pre-verifier saves `satisfied` and never invokes Claude.
- A failing pre-verifier invokes Claude once.
- A passing post-verifier saves `success` with before and after evidence.
- A failing post-verifier saves `incomplete`.
- A Claude failure stays `error` and does not run the post-verifier.
- A gated planning run saves `pending` without a post-verifier.
- Approval skips execution and marks `superseded` when the goal recovered.
- Approval executes and verifies when the goal remains false.
- A legacy agent follows the existing path unchanged.

### CLI and store tests

- `check` rejects an agent without desired-state fields.
- `check` reports pass and fail correctly.
- New evidence round-trips through JSON.
- Old run JSON still loads.

### Release verification

```text
go test ./...
go vet ./...
go build ./cmd/watchd
```

A smoke test uses a temporary `claude` shim and an agent whose verifier checks
for a temporary file. The shim creates the file and emits valid Claude JSON.
The first run must invoke the shim and save `success`. The next run must save
`satisfied`, and the shim invocation count must remain one.

## Acceptance criteria

- A true goal never invokes Claude.
- A false goal cannot be reported as successful without a passing post-verifier.
- Approval never executes a stale plan after the goal has recovered.
- Every goal claim stores inspectable before and after evidence.
- Existing agents and stored runs remain compatible.
- The core remains a single binary with no new runtime dependency.
- Tests, vet, and build pass.

## Deferred work

The verifier creates the objective signal needed for later intelligence. A
future `watchd evolve` can propose candidate agent instructions, evaluate them
against held-out goal cases, and request promotion. V1.1 does not mutate agent
files, choose its own metrics, auto-promote candidates, allocate budgets, add
retries, or introduce a dashboard.
