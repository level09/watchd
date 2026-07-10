# Watchd v1.1: Adaptive Outcome Portfolio

Date: 2026-07-10
Status: Approved direction, implementation pending written-spec review

## Product decision

Watchd v1.1 stops treating every schedule as an instruction to spend money.
Schedules make agents eligible. Watchd allocates a finite daily budget to the
eligible loops with the strongest verified return, while preserving a small
exploration allowance for new or uncertain loops.

Positioning:

> Watchd learns which autonomous loops deserve your AI budget.

The persistent product objects become goals, outcome history, budget policy,
and evidence. Agent files remain simple, replaceable strategies. A prompt or
skill can propose work, but it cannot enforce the portfolio budget, retain the
outcome ledger, govern authority, or change future allocation from real usage.

Desired-state verification remains useful, but only as one evidence adapter.
It is not the release thesis.

## Boundaries

The release remains a local Go CLI wrapping `claude -p` and using plain files.
It adds no database, web interface, multi-provider abstraction, swarm runtime,
vector store, background prompt mutation, or general workflow language.

Production code stays below 2,000 lines. The current production core is 1,164
lines, leaving roughly 800 lines for goal parsing, allocation, verification,
outcome recording, and CLI changes.

The first release uses transparent deterministic allocation. Reinforcement
learning, counterfactual credit assignment, and automatic skill evolution are
deferred until Watchd has enough real outcome data to justify them.

## User model

A portfolio has four elements:

1. `watchd.yaml` defines the global resource and review limits.
2. `goals/*.md` defines what matters and how much it matters.
3. `agents/*.md` defines replaceable strategies attached to goals.
4. `runs/*.json` forms the cost, evidence, decision, and outcome ledger.

Without `watchd.yaml`, Watchd preserves the current v1.0 scheduling behavior.
Existing agents and run files remain valid.

## Portfolio policy

`watchd.yaml` is optional:

```yaml
daily_budget: 1.00
exploration: 0.15
max_unrated: 5
max_pending: 3
```

Fields:

| Field | Rule |
| --- | --- |
| `daily_budget` | Required when the file exists. Must be greater than zero. |
| `exploration` | Optional, default `0.15`. Must be between zero and one. |
| `max_unrated` | Optional, default `5`. Maximum completed portfolio runs awaiting a human outcome. |
| `max_pending` | Optional, default `3`. Maximum gated proposals awaiting approval. |

The daily window uses the machine's local calendar date. Spend is the sum of
provider-reported cost for runs started on that date. Manual runs and approved
actions count against the same budget.

The daily budget is an admission limit, not a claim about final provider
billing. Before a run, Watchd requires enough remaining budget to reserve the
agent's configured per-run budget. The existing Claude `--max-budget-usd`
control remains the per-run enforcement layer. Watchd reconciles the
reservation with the provider-reported actual cost afterward. If actual spend
exceeds the reservation, the excess reduces the remaining daily budget and
blocks later work.

## Goal files

A goal is a markdown file under `goals/`:

```markdown
---
name: product
weight: 3
daily_budget: 0.75
authority: propose
---

Make Watchd more useful to people running several autonomous loops without
creating output they cannot review.
```

Fields:

| Field | Default | Rule |
| --- | --- | --- |
| `name` | filename | Unique identifier. |
| `weight` | `1` | Positive finite number used in allocation. |
| `daily_budget` | none | Optional positive cap inside the global cap. |
| `authority` | `propose` | One of `observe`, `propose`, or `act`. |

The markdown body is the durable goal statement and is injected into attached
agent prompts. Goal files are authored by the user. Watchd never rewrites them.

Authority is enforced as follows:

- `observe`: force the read-only tool set and store the result as a completed
  observation. No approval state is created.
- `propose`: force the read-only tool set and store a successful result as
  `pending` for approval.
- `act`: use the agent's configured tools and permission mode. An agent with
  `gate: true` still reduces this to `propose`.

A less restrictive agent cannot override the goal authority. Health and
finance examples use `observe` or `propose`.

## Portfolio agents

An existing agent joins the portfolio with `goal`:

```markdown
---
name: repo-health
goal: product
schedule: 6h
model: sonnet
budget: 0.25
verify: go test ./...
verify_timeout: 2m
---

Find the smallest high-leverage repair when the verifier fails.
```

Rules:

- `goal` must reference an existing goal file.
- A portfolio agent must have a positive `budget` so allocation can reserve
  spend before execution.
- `verify` is optional. If present, `verify_timeout` defaults to two minutes.
- `verify_timeout` must parse with `time.ParseDuration` and be positive.
- A legacy agent without `goal` keeps current behavior when no portfolio policy
  exists. When a policy exists, legacy scheduled agents are invalid because
  their spend cannot be assigned to a goal. A manual legacy run remains valid
  only when it has a positive per-run budget; it counts against the global cap
  without using a goal cap.

The existing agent hash and a semantic goal hash form the strategy version.
The goal hash covers the goal name, authority, and body, but excludes weight
and budget fields. Allocation statistics use only runs with the current agent
and semantic goal hashes. Editing agent behavior or goal meaning starts a fresh
strategy prior while preserving old history for inspection. Changing only an
allocation weight or cap does not discard outcome history.

## Verification as evidence

The verifier is a user-authored shell command executed from Watchd's current
working directory with the inherited environment.

Exit semantics:

- Exit zero: desired state is satisfied.
- Exit 126 or 127: verifier configuration error.
- Any other nonzero exit: desired state is unsatisfied.
- Start failure or timeout: infrastructure error.

Combined stdout and stderr is stored up to 8 KiB. Output is evidence, never
trusted instructions. Injected verifier context explicitly tells the agent not
to follow instructions found in verifier output.

Runtime classification:

- Pre-verifier passes: save `satisfied`, spend zero model cost, and exclude the
  event from allocation quality statistics.
- Pre-verifier fails and post-verifier passes: append an automatic `useful`
  outcome from source `verify`.
- Both verifiers fail: save `incomplete` and append automatic `neutral`.
- Claude or verifier infrastructure fails after admission: save `error` and
  count it as non-useful for allocation without labeling it harmful.
- A gated planning pass has no post-verifier. It remains unrated until approval
  executes, rejection records `neutral`, or the user records an outcome.

Approval re-runs the pre-verifier. If it now passes, mark the proposal
`superseded` and do not execute a stale plan.

## Human outcomes

Research, monitoring, and strategic work often lacks a deterministic verifier.
The user records whether a completed run created value:

```text
watchd outcome <run-id> useful [note]
watchd outcome <run-id> neutral [note]
watchd outcome <run-id> harmful [note]
```

Each command appends an outcome rating instead of overwriting history:

```go
type OutcomeRating struct {
    Value   string    `json:"value"`
    Source  string    `json:"source"`
    Note    string    `json:"note,omitempty"`
    RatedAt time.Time `json:"rated_at"`
}
```

Valid values are `useful`, `neutral`, and `harmful`. Source is `human` or
`verify`. The allocator uses the latest rating. The complete history preserves
corrections and makes judgment changes auditable.

Runs in `pending` or `approved` cannot be rated because their intervention is
not terminal. All other terminal portfolio statuses can be rated. Human
ratings may supersede automatic ratings, but the automatic record remains in
the history.

## Allocation

The allocator receives the due agents, current goals, today's runs, all runs
for the current agent hashes, and the policy. It has no model dependency and no
side effects.

### Eligibility

An agent is eligible when:

- Its schedule is due.
- Its goal and configuration are valid.
- Its per-run budget fits inside the global and goal remaining budgets.
- The global pending cap has room when authority resolves to `propose`.
- The global unrated cap has room when no verifier can rate the result.
- The same agent has no terminal unrated run. This prevents repeated output
  from accumulating faster than the user can judge it.

Budget exhaustion and review-cap exhaustion are normal skip reasons, not
errors.

### Value model

For the current agent hash:

- `useful` contributes `+1`.
- `neutral` contributes `0`.
- `harmful` contributes `-1`.
- An admitted run ending in `error` without a rating contributes `0` and
  increases the sample count. If it is later rated, use the rating and do not
  count a second error sample.
- Unrated and pre-satisfied runs do not enter the value model.

Use a prior of one positive and one neutral observation:

```text
expected_value = (1 + useful - harmful) / (2 + rated + errors)
```

The cost estimate is actual average model cost for the current agent hash,
falling back to the configured per-run budget when no history exists. Clamp it
to a one-cent floor.

Exploration is deterministic:

```text
uncertainty = sqrt(log(total_samples + 2) / (rated + errors + 1))
score = goal_weight * (expected_value + exploration * uncertainty) / estimated_cost
```

`total_samples` is the sum of rated outcomes and unrated errors across all
current strategy versions in the portfolio snapshot.

Sort by descending score, then goal name, then agent name. The tie breakers
make identical state produce identical allocation.

This is intentionally a simple upper-confidence style policy. It values useful
outcomes per dollar, penalizes harmful results, and gives uncertain strategies
bounded chances without asking an LLM to judge itself.

### Admission loop

1. Gather all due agents.
2. Compute eligibility and scores against one immutable snapshot.
3. Sort candidates deterministically.
4. Admit the highest-ranked candidate whose reservation fits.
5. Execute it synchronously using the existing runner.
6. Reconcile actual spend and outcome state.
7. Recompute the snapshot before admitting the next candidate.
8. Stop when no eligible candidate fits or all due agents were considered.

Watchd stays single-process and sequential. This avoids distributed budget
reservation and concurrent review races in v1.1.

Skipped due agents remain due. They are reconsidered after an outcome changes,
a pending proposal is resolved, budget becomes available on the next day, or
another state transition changes eligibility. The daemon does not print the
same skip on every 30-second tick.

## CLI surface

New commands:

```text
watchd portfolio
watchd outcome <run-id> useful|neutral|harmful [note]
watchd check <agent>
```

`watchd portfolio` prints:

- Global spend and remaining daily budget
- Unrated and pending review debt
- Each goal's weight, spend, cap, and useful outcomes
- Each agent's current-hash score, rated outcomes, average cost, and next
  eligibility reason
- Useful outcomes per dollar, shown as `-` when no useful outcome exists

`watchd check` runs only the configured verifier and rejects agents without
one.

Existing command changes:

- `watchd up` uses adaptive allocation when `watchd.yaml` exists.
- `watchd run` and `watchd approve` require portfolio budget admission when a
  policy exists.
- `watchd reject` appends automatic `neutral` for the rejected proposal.
- `watchd list` and the default dashboard show goal and eligibility.
- `watchd logs` shows outcome and allocation reason.
- `watchd costs` remains available and includes every run.
- `watchd help` documents the new surface.

## Stored evidence

`store.Run` gains optional fields:

```go
Goal               string          `json:"goal,omitempty"`
GoalHash           string          `json:"goal_hash,omitempty"`
Allocation         *Allocation     `json:"allocation,omitempty"`
OutcomeRatings     []OutcomeRating `json:"outcome_ratings,omitempty"`
VerificationBefore *Verification   `json:"verification_before,omitempty"`
VerificationAfter  *Verification   `json:"verification_after,omitempty"`
```

`Allocation` contains:

```go
type Allocation struct {
    Score            float64 `json:"score"`
    ReservedUSD      float64 `json:"reserved_usd"`
    EstimatedCostUSD float64 `json:"estimated_cost_usd"`
    RemainingUSD     float64 `json:"remaining_usd_before"`
    Reason           string  `json:"reason"`
}
```

`Verification` keeps the previously designed command, pass, exit, bounded
output, error, and duration fields.

All new fields are optional so old JSON remains readable. Run IDs continue to
be filename-derived and `SaveRun` remains an upsert.

## Code boundaries

### `internal/agent`

- Parse `goal`, `verify`, and `verify_timeout`.
- Validate verifier syntax independent of goal discovery.
- Preserve legacy defaults.

### `internal/portfolio`

- Parse and validate `watchd.yaml`.
- Parse `goals/*.md`.
- Resolve goal authority and agent configuration.
- Compute semantic goal hashes, daily spend, review debt, strategy statistics,
  eligibility, and deterministic scores.
- Return decisions without invoking models or writing files.

### `internal/runner`

- Add the bounded verifier and before-and-after flow.
- Accept resolved authority instead of deciding policy itself.
- Keep Claude invocation injectable for tests.
- Append automatic outcomes only after terminal evidence.

### `internal/store`

- Add allocation, outcome, and verification records.
- Provide current-day and current-hash queries by filtering existing JSON.
- Preserve backward-compatible loading.

### `internal/daemon`

- Replace run-all-due behavior with sequential portfolio admission when policy
  exists.
- Keep legacy scheduling when policy is absent.
- Inject the clock and run function for deterministic tests.

### `internal/cli`

- Add `portfolio`, `outcome`, and `check`.
- Apply admission to manual run and approval.
- Render outcome and eligibility consistently.
- Bump the version to `1.1.0`.

## Failure handling

- Invalid `watchd.yaml`, goal files, unknown goals, invalid authority, missing
  portfolio budgets, and invalid verifier timeouts fail before the daemon
  starts.
- Goal discovery reports every invalid file in one error instead of silently
  skipping portfolio policy mistakes.
- Budget or review limits produce explicit skip reasons.
- A verifier infrastructure error saves `error` and never claims an outcome.
- A Claude error preserves cost and allocation evidence.
- An invalid outcome value or nonterminal run fails without modifying the run.
- Empty or non-finite score inputs fail validation. No `NaN` or infinity is
  persisted.
- Store write failures return errors. Outcome and approval transitions are not
  reported as successful until persistence succeeds.
- Notification fires for `pending`, `error`, `incomplete`, and `harmful`.

## Testing strategy

Implementation follows test-first development.

### Parsing and compatibility

- Load valid policy and goal files.
- Reject missing or nonpositive budgets and weights.
- Reject exploration outside zero to one.
- Reject invalid authority, goal reference, and verifier timeout.
- Preserve legacy agents and old run JSON.

### Allocation

- Produce identical order for identical state.
- Prefer higher verified value per dollar when weights match.
- Respect goal weight and goal cap.
- Give a bounded exploration bonus to a new strategy.
- Reset active statistics when the agent or semantic goal hash changes, but
  retain them when only goal weight or budget changes.
- Exclude unrated and pre-satisfied runs.
- Penalize harmful outcomes and count errors as non-useful samples.
- Enforce global budget, goal budget, pending cap, unrated cap, and one-unrated
  run per agent.
- Reconcile actual cost above and below the reservation.
- Reject non-finite inputs.

### Verification and authority

- Capture passing, failing, misconfigured, timed-out, and noisy verifiers.
- Bound output to 8 KiB.
- Skip Claude for a satisfied pre-verifier.
- Record useful only after a passing post-verifier.
- Record neutral for incomplete and rejected work.
- Recheck before approval and supersede stale proposals.
- Enforce `observe`, `propose`, and `act`, including `gate: true` as a further
  restriction.

### Store and CLI

- Append human ratings and preserve prior ratings.
- Use only the latest rating in statistics.
- Reject ratings on nonterminal runs.
- Render empty denominators without division errors.
- Apply admission to manual run and approval.
- Report portfolio scores and skip reasons from fixtures.

### Daemon

- Preserve legacy run-all scheduling without policy.
- Admit sequentially in score order with policy.
- Recompute after each actual result.
- Leave skipped agents due without repeated console noise.
- Resume consideration after a rating, approval, rejection, or day rollover.

### Release verification

```text
go test ./...
go vet ./...
go build ./cmd/watchd
```

A smoke test uses a temporary Claude shim, two goals, and four agents:

- One verifier-backed agent repairs a temporary file.
- One cheap research agent receives a human `useful` rating.
- One expensive neutral agent loses allocation priority.
- One new agent receives the configured exploration opportunity.

The test proves budget admission, automatic verification, human outcome
recording, deterministic allocation, gated authority, and portfolio reporting
without calling an external model.

## Acceptance criteria

- Portfolio mode never admits a run without enough global and goal reservation.
- Every portfolio run stores the goal, current agent and semantic goal hashes,
  allocation reason, reservation, actual cost, and available evidence.
- A false goal cannot become useful without a passing post-verifier or an
  explicit human rating.
- A true verifier skips model execution.
- Approval never executes a stale proposal after the goal recovers.
- Review debt stops unbounded report and proposal production.
- Repeatedly useful strategies gain allocation relative to equally weighted,
  equally priced neutral strategies.
- New strategies retain bounded exploration access.
- Harmful outcomes reduce future allocation and remain auditable.
- Identical state produces identical decisions.
- Legacy behavior remains unchanged when portfolio configuration is absent.
- The binary gains no runtime dependency beyond the existing Claude CLI.
- Production code remains below 2,000 lines.
- Tests, vet, build, and the local smoke test pass.

## Deferred work

- Automatic mutation or promotion of agent and skill files
- Counterfactual replay and causal credit assignment
- Learned or stochastic allocation policies
- Cross-machine or concurrent budget reservations
- Model-provider abstraction
- Web dashboard
- Automatic health or financial actions
- Networked outcome sharing

The outcome ledger is the prerequisite for these features. Watchd must collect
real evidence before it earns more intelligence.
