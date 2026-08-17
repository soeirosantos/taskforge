# TaskForge — Execution Notes

Orchestrator record for the implementation run of `SPEC.md` against `PLAN.md`.
Required by `.claude/TASK_PLANNING_INSTRUCTIONS.md` §13. Written during the run,
not reconstructed afterwards.

Arm: `experiment/go-job-processing-service`.

---

## Pre-flight

Run before creating T1. All checks passed.

| Check | Result |
|---|---|
| Branch | `experiment/go-job-processing-service` |
| `git status --porcelain .claude/` | empty — gate config committed by the user before the run |
| `TEST_COMMAND` | `go test ./...` |
| `TEST_TIMEOUT_SECONDS` | 300 (hook timeout in `settings.json` is 360) |
| Go toolchain | `go1.23.5 linux/arm64` — not modified at any point |

Note on gate coverage: the `TaskCompleted` gate runs `go test ./...` only. It
does **not** cover `-race`, `go vet`, `gofmt`, or `go build`. Those come from the
per-task verification command, run by the orchestrator after each worker returns.

### Container/toolchain changes made during the run

- **Planning phase:** the shared Go module and build caches at
  `/home/agent/go/pkg/mod` were warmed by probing `modernc.org/sqlite` in a
  scratch directory outside the repository. This does not survive the container,
  so a cold reproduction of this arm pays a one-time module download. No
  toolchain was installed or switched. Nothing under `/work` was modified by the
  probes.
- No other container or toolchain changes so far.

### APPARATUS CHANGE — worker turn budgets recalibrated mid-run

Made deliberately by the user after T1 hit `STOP / human escalation`, as the
chosen resolution to that escalation (option 3 of the three offered in
`.claude/escalations/1.md`). This is a change to the measured apparatus and is
recorded here for exactly that reason.

| Profile | Before | After |
|---|---|---|
| `worker-haiku` | 8 | 15 |
| `worker-sonnet` | 15 | 40 |
| `escalation-opus` | 15 | 50 |

**Evidence it was derived from**, not guessed: `escalation-opus` produced 1208
lines across 9 files in 15 turns and stopped ~2–4 turns short of a compiling
tree. A clean run of a T1-sized task therefore needs ~18 turns before any
debugging; each failing-test cycle (read → edit → re-run) costs a further 2–3
turns, so room for 8–10 cycles puts the normal worker at 40. `escalation-opus`
additionally pays 3–5 turns diagnosing the previous attempt before it writes
anything, which is why it is set above the normal worker rather than equal to it
— at 15/15 it was doing strictly more work on the same budget.

**Rationale.** Both T1 failures were turn exhaustion on a well-understood
problem, not difficulty. `maxTurns` is a runaway-loop backstop, not a
productivity dial; it should sit high enough that exhaustion is a signal of
pathology rather than a routine outcome. At 15 it was firing routinely.

**Consequences for the experiment — read before comparing arms.**

1. This arm no longer shares the warmup arms' apparatus. The warmups ran at
   sonnet/opus 15 and haiku 8. Any comparison of task-completion or escalation
   rates across those arms and this one is invalid unless the warmups are re-run
   at the new budgets.
2. The changes are currently **uncommitted and local to this branch's working
   tree**. For future arms to share the apparatus they must land on `main` and be
   inherited, the same way `verify-unit-tests.sh` is. Left to the user, who
   commits from outside the container.
3. `git diff main..HEAD -- .claude/hooks/` is unaffected — it still shows only
   `test-command.conf`. The tripwire's invariant covers `hooks/`, not `agents/`,
   so it will not flag this divergence. Worth knowing when reading the tripwire's
   silence as evidence.

**IMPORTANT — the change did not take effect in the running session.** The first
worker dispatched after the edit stopped at the same point as its predecessors
despite being configured for 40 turns:

| Run | Configured `maxTurns` | Assistant turns before cutoff | Wall clock |
|---|---|---|---|
| T1 attempt 1 (sonnet) | 15 | 20 | 128s |
| T1 attempt 2 (opus) | 15 | 17 | 265s |
| T1 attempt 3 (sonnet, post-change) | **40** | 19 | 31s |

Every one of the three transcripts terminates with `stop_reason: "tool_use"` —
the model was mid-tool-call when the loop was cut, with no natural `end_turn` in
any of them. Attempt 3 was configured for 40 turns and stopped at 19, in the same
band as the two runs configured for 15.

**Conclusion: subagent definitions are read at session start, so editing
`.claude/agents/*.md` mid-session does not affect subagents spawned later in that
same session.** The files on disk are correct; this session is not using them.
The new budgets require a fresh session before they apply. Until then every
worker is still effectively bounded at the old limit, and tasks must be scoped to
fit it.

This also means attempt 3 is *not* evidence about the 40-turn budget — it is a
third data point about the 15-turn one. It failed the same way as the others:
it consumed its budget reading the 1208 existing lines to verify them, exactly as
instructed, and was cut off just as it identified the one-line fix.

**Calibration probe after resuming the session — the new budget IS in force.**

The session was resumed (not restarted) and a probe dispatched before any real
work: a `worker-sonnet` instructed to make 30 separate Bash calls, one per turn,
each appending its own number to a file, with batching/loops/`seq` forbidden.

Result: the file contains exactly the sequence 1..30, 30 tool uses, ~52s, and the
agent **finished naturally rather than being cut off**. Every pre-change run was
cut at 17–20 turns; this one passed 30 with headroom.

Conclusions:

1. Subagent definitions are re-read when the session process restarts. **Resuming
   is sufficient — a full restart is not required** to pick up
   `.claude/agents/*.md` edits.
2. The recalibrated budgets (sonnet 40, opus 50, haiku 15) are in force for all
   work from this point. T2 onward run under them; T1 ran entirely under the old
   15/8 limits.

**T1's escalation ladder is treated as reset by this change.** Both prior
failures are attributable to the configuration rather than to the model tier, so
the re-run starts again at the normal tier (`worker-sonnet`) rather than going
straight to Opus. The record at `.claude/escalations/1.md` stands as the
historical account of the attempts made under the old budget.

---

## Task log

<!-- One entry per task: profile, outcome, orchestrator-run verification, notes. -->

### T1 — Domain model, job execution, and module initialization

- Initial profile: `worker-sonnet`
- Status: **COMPLETE** after 4 attempts (2 sonnet, 1 opus, 1 narrow sonnet
  repair) and one human escalation. Record at `.claude/escalations/1.md`.

**Attempt 4 — `worker-sonnet`, narrow repair — SUCCESS.** 5 tool uses, 14s.
Scoped to "apply this one-line fix as your first action, then iterate on test
failures", with no instruction to survey the package. It applied
`bytes.NewReader` and the suite passed first try; no second failure surfaced.

Orchestrator-run verification:

```
$ test -z "$(gofmt -l .)" && go build ./... && go vet ./... && go test -race ./internal/jobs/ -count=1
ok  	taskforge/internal/jobs	1.140s
EXIT=0

$ go test ./...            # the gate command
ok  	taskforge/internal/jobs	0.130s
GATE_EXIT=0
```

**Substance check (the gate cannot see this).** 25 top-level tests, 80 subtests.
Each acceptance criterion traced to a real assertion:

| # | Criterion | Evidence |
|---|---|---|
| 1 | Transition table | `TestCanTransitionCoversEveryPair` cross-checks all 25 pairs against `legalTransitions`, declared **independently in `state_test.go`** against the implementation's separate `validTransitions` in `state.go`. Not tautological — verified both symbols exist and differ. Plus terminal-state and self-transition tests |
| 2 | Hash | Exact digest `b94d27b9…efcde9` asserted, and the full `{"sha256":…}` result shape |
| 3 | Delay cancellation | Cancels 20ms into a 30000ms delay; asserts `context.Canceled`, nil result, elapsed < 500ms |
| 4 | Fail | Exact code and message, plus serialized `{"code":"INTENTIONAL_FAILURE","message":"job failed intentionally"}` |
| 5 | Payload validation | Unknown fields, missing fields, `null`, wrong types, and range boundaries 99/100/30000/30001 |
| 6 | Field set | `specFieldOrder` pins all 12 SPEC §11 fields in order; a token-stream walk asserts encoded order |

`TestTimestampLayoutIsFixedWidthAndSortable` covers the RFC3339Nano ordering trap
flagged in planning — the sortability property later tasks depend on is now
pinned by a test rather than by a comment.

**Not a vacuous pass.** The suite exercises real behaviour; both the gate command
and the fuller `-race` command were run by the orchestrator after the worker
reported.

**Cost of T1:** 4 worker dispatches, ~145k subagent tokens, ~7.5 min of worker
wall clock, 1 human escalation. Three of the four attempts died to the same
cause.

**Lesson carried into T2–T9.** The decisive variable was *prompt scope*, not
model tier. The three failed attempts were all told to read and understand
before acting; the successful one was told to make a specific change first and
discover the rest from test output. Until the recalibrated budgets are actually
in force, dispatch prompts must lead with a concrete first edit and let
verification drive the remainder, rather than asking a worker to survey a package
before touching it.

**Attempt 1 — `worker-sonnet` — FAILED (bounded attempt exhausted).**

The worker's final output cut off mid-sentence: *"Let me verify Go's
`json.Number` decoding behavior for type-safety before committing to this
approach."* 18 tool uses, ~128s. It exhausted its turn limit while investigating
a single design detail and **never created the deliverable**.

Delivered: `go.mod` (`module taskforge`, `go 1.23.5`) and the `.gitignore`
additions — both correct. **`internal/jobs/` was never created.** No domain
types, no executors, no tests.

Orchestrator-run verification (not the worker's claim):

```
$ test -z "$(gofmt -l .)" && go build ./... && go vet ./... && go test -race ./internal/jobs/ -count=1
go: warning: "./..." matched no packages
no packages to vet
EXIT=1

$ go test ./...            # the TaskCompleted gate command
go: warning: "./..." matched no packages
no packages to test
EXIT=1
```

Note that the gate would have refused this task anyway: with `go.mod` present but
no packages, `go test ./...` exits 1. This matches the planning-phase finding and
is why module bootstrap was folded into T1 rather than standing alone as its own
task.

**Diagnosis.** Not a capability failure — a scope-of-inquiry failure. The worker
stopped to resolve whether `json.Number` was needed to reject a non-integer
`milliseconds` value, and the investigation consumed the budget it needed for the
implementation. The task guidance stated the *requirement* ("reject non-integers
such as `100.5`") but not the *mechanism*, and the mechanism turned out to be
non-obvious enough to derail the attempt.

**Attempt 2 — `escalation-opus` — FAILED (bounded attempt exhausted).**

19 tool uses, ~265s. Consumed the resolved payload-decoding finding correctly and
implemented the complete package — 1208 lines across 9 files — writing
implementation first and tests last. It exhausted its turns during the final test
file. Its last words: *"Now the tests, one file per acceptance area."*

Orchestrator-run verification:

```
$ test -z "$(gofmt -l .)" && go build ./... && go vet ./... && go test -race ./internal/jobs/ -count=1
vet: internal/jobs/job_test.go:192:25: undefined: newByteReader
EXIT=1

$ go test ./...            # the gate command
internal/jobs/job_test.go:192:25: undefined: newByteReader
FAIL	taskforge/internal/jobs [build failed]
GATE_EXIT=1
```

One undefined identifier, referenced once and defined nowhere. Because the
package does not compile, **none of the six acceptance criteria are verified** —
the other 1200 lines may be correct, but there is no evidence either way.

**Outcome: STOP / human escalation** per the `CLAUDE.md` bounded-execution
policy. There is no autonomous tier after Opus. Record written to
`.claude/escalations/1.md`. T2–T9 are blocked; none were started.

**Root-cause diagnosis — this is a planning defect, not a worker defect.** Two
different models at two capability tiers both exhausted a 15-turn budget on the
same task, at different points and for different proximate reasons. T1 bundles
module initialization, five domain concerns, and tests for six acceptance
criteria — roughly 1200 lines. The evidence points at task size against the
configured bound, not at task difficulty. T3, T5 and T7 are comparably large and
carry the same risk.

The orchestrator did **not** repair the one-line error itself: doing so would
make it both implementer and verifier of the same code, the exact
self-certification the policy exists to prevent.

**Orchestrator probe run before escalating** (scratch module, outside the repo),
to hand the escalation a verified fact instead of the same open question:

```
{"milliseconds":5000}      -> err=<nil>
{"milliseconds":100.5}     -> cannot unmarshal number 100.5 into ... int64
{"milliseconds":1e3}       -> cannot unmarshal number 1e3 into ... int64
{"milliseconds":5000.0}    -> cannot unmarshal number 5000.0 into ... int64
{"milliseconds":"5000"}    -> cannot unmarshal string into ... int64
{"milliseconds":null}      -> err=<nil>     <-- no error
{"ms":5000}                -> unknown field "ms"
{}                         -> err=<nil>     <-- no error
```

So: a plain `int64` field plus `DisallowUnknownFields` already rejects every
non-integer form, and `json.Number` is unnecessary. The real gap is the last two
lines — `null` and *absent* both decode silently to the zero value, so
distinguishing "field missing" requires a pointer field (`*int64`), not
`json.Number`. This was passed to the escalation.

---

### T2 — Persistence foundation (`internal/store`)

- Initial profile: `worker-sonnet`
- Status: **COMPLETE**, 1 dispatch + 1 send-back. First task run under the
  recalibrated budgets and the first written to the new dispatch pattern
  (`PLAN.md` §0.4).

**Dispatch 1 — SUCCESS with one unmet criterion.** 18 tool uses, ~175s. Produced
`store.go`, `errors.go`, `job.go`, `list.go`, `store_test.go`. Gate passed.

Orchestrator-run verification:

```
$ test -z "$(gofmt -l .)" && go build ./... && go vet ./... && go test -race ./internal/store/ -count=1
ok  	taskforge/internal/store	1.185s
EXIT=0

$ go test ./...
ok  	taskforge/internal/jobs	(cached)
ok  	taskforge/internal/store	0.125s
GATE_EXIT=0
```

`go.mod` pins `modernc.org/sqlite v1.39.0`, directive still `go 1.23.5`. The
worker reported that its first `go get` was stripped by `go mod tidy` because no
source imported the driver yet, and that it re-ran the pinned `go get` after
writing the code — correct handling, and the pin held.

**Substance check found a real gap the gate could not see.** Criterion 3 requires
the populated field shape to survive close-and-reopen.
`TestCreateGet_RoundTripsAllFields` did close and reopen, but covered the *nil*
shape; `TestCreateGet_RoundTripsNonNilOptionalFields` used a single open handle,
so `Result`, `Err`, `StartedAt`, `FinishedAt` and a non-zero `AttemptCount` were
never read back across a reopen. SPEC §39 names result data, error data and
attempt counts as things that must survive a reopen specifically, so this was
substantive.

**This is the first concrete evidence that the substance review earns its
place:** the gate was green on a suite that did not test what SPEC §39 requires.

**Send-back (not an escalation).** The worker had neither exhausted its budget
nor failed the gate — it passed the gate and missed one criterion, found in
review. The `CLAUDE.md` ladder triggers on failure or exhaustion, neither of
which occurred, so the correct action was to return the specific defect to the
same agent rather than escalate. Used `SendMessage` to resume it with its context
intact: 4 tool uses, ~26s.

Re-verified after the fix: the non-nil test now opens, populates, `Close()`s,
reopens a second `Store` on the same path, and asserts `AttemptCount == 2`,
`Result`/`Err` byte-identical, and `StartedAt`/`FinishedAt` via
`jobs.FormatTime`. Suite green, gate green.

**Substance checks that came back sound on the first dispatch:**

- The `created_at` tie-break test is genuine — two jobs share an identical
  `created_at`, are inserted out of id order, and their ids are normalized so the
  expected order cannot accidentally match insertion order. This was the
  assertion most likely to be faked, and it was not.
- Scope held exactly. Exported surface is `Open/Close/Create/Get/List/Ping` with
  no claim/complete/fail/cancel/retry/recovery leaking in from T3, confirmed via
  `go doc -all`.
- `internal/jobs` reported `(cached)` by `go test ./...` after T2's changes,
  which is positive evidence that T1's package was not modified.

**Cost of T2:** 1 dispatch + 1 send-back, ~91k subagent tokens, ~3.4 min worker
wall clock, no escalation. Compare T1: 4 dispatches, ~145k tokens, 1 human
escalation.

### T3 — Atomic state transitions and startup recovery (`internal/store`)

- Initial profile: `worker-sonnet`
- Status: **COMPLETE**, 1 dispatch, no send-back, no escalation. 35 tool uses,
  ~291s, ~91k tokens.

The plan's highest-risk Sonnet task, and it passed on the first attempt.

Orchestrator-run verification:

```
$ test -z "$(gofmt -l .)" && go build ./... && go vet ./... && go test -race ./internal/store/ -count=1
ok  	taskforge/internal/store	1.653s
EXIT=0

$ go test ./...
ok  	taskforge/internal/jobs	(cached)
ok  	taskforge/internal/store	0.262s
GATE_EXIT=0
```

Stressed harder than the worker did: concurrency tests at `-count=10` and the
full package at `-count=3`, both clean. Not flaky.

**Substance checks — all four things most likely to be faked here were real:**

1. **No read-then-write anywhere (SPEC §15).** Audited every SQL statement in
   `transition.go`. The only `SELECT` is the subquery *inside* the claim's
   `UPDATE`; everything else is a guarded `UPDATE`. Reads happen only via
   `classifyMiss`, after an update has already affected zero rows, purely to tell
   `OutcomeLost` from `OutcomeNotFound` — never to decide whether to write. That
   is the one read the dispatch explicitly authorized, and it stayed inside those
   bounds.
2. **Claim is genuinely one statement.** `UPDATE jobs SET status='RUNNING',
   attempt_count = attempt_count + 1, started_at=?, updated_at=? WHERE id =
   (SELECT id FROM jobs WHERE status='QUEUED' AND attempt_count < ? ORDER BY
   queued_at ASC, id ASC LIMIT 1) RETURNING <cols>` — selection, transition,
   increment, and fetch in a single statement, so SPEC §14's atomic-increment
   requirement is structural rather than argued.
3. **The concurrency tests are real races**, not sequential assertions: 8
   goroutines, `sync.WaitGroup`, `atomic.AddInt32` win counter, asserting exactly
   one winner and `attempt_count == 1`.
4. **The tie-break is genuinely exercised** — four jobs, two sharing a `queued_at`,
   ids normalized so the expected claim order cannot accidentally match insertion
   order.

Recovery preserves `attempt_count`, `created_at`, `queued_at` and `started_at`,
sets `result` NULL and `finished_at`, and writes the exact
`INTERRUPTED_EXECUTION` message — all asserted.

**Trend across the three completed tasks.** The dispatch pattern from `PLAN.md`
§0.4 is holding: T1 (old budget, comprehend-first prompts) took 4 dispatches and a
human escalation; T2 took 1 dispatch plus a send-back; T3, the riskiest of the
three, took 1 clean dispatch. Supplying the verified primitive up front — the
8-goroutine `RowsAffected` probe result — appears to be what kept the worker from
re-deriving the concurrency approach.

### T4 — Worker pool and cancellation coordination (`internal/worker`)

- Initial profile: **`escalation-opus` (direct)** — the plan's only direct-Opus
  assignment
- Status: **COMPLETE**, 1 dispatch. 53 tool uses, ~995s, ~116k tokens, 1266 lines.

Dispatched with an explicit instruction to **skip the diagnostic step** in the
escalation profile's standard procedure, since that profile assumes it is
repairing a prior attempt and there was none. Without that, it would have spent
turns hunting for a previous state.

Orchestrator-run verification:

```
$ test -z "$(gofmt -l .)" && go build ./... && go vet ./... && go test -race ./internal/worker/ -count=1
ok  	taskforge/internal/worker	4.273s
EXIT=0

$ go clean -testcache && go test ./...      # cold, whole suite
ok  	taskforge/internal/jobs	0.124s
ok  	taskforge/internal/store	0.306s
ok  	taskforge/internal/worker	1.952s
real	0m2.141s
GATE_EXIT=0
```

Cold suite 2.1s against the gate's 300s budget — ample headroom as more tests land.

**The crux, and how it was resolved.** The dispatch flagged that `store.Claim`
selects *and* transitions in one statement, so a worker cannot know the job id
until `RUNNING` is already persisted — making literal "register before claim"
unimplementable without changing the store, which was forbidden. The worker
identified this correctly and closed the window with two mechanisms instead:
register before any work and release only after the terminal transition, plus
`confirmRunning`, a re-read of the persisted row between registration and
execution. Its correctness argument is a case split on the registry mutex, which
totally orders the canceller's lookup against the worker's registration.

**Mutation test — reproduced independently, not taken on trust.** The worker
claimed it had disabled `confirmRunning` and found that its timing-based stress
test passed anyway while a dedicated test failed deterministically. That is
exactly the kind of claim that must be checked, so the orchestrator reproduced
it: backed up `pool.go`, disabled the guard, and ran both tests.

```
# guard disabled
--- FAIL: TestCancelBetweenClaimAndRegistrationNeverRuns (3.05s)
    pool_test.go:335: execute took 3.011351495s: the worker ran a job whose
                      CANCELLED state was already persisted

# same mutation, timing-based test, -count=5
ok  	taskforge/internal/worker	10.547s
```

Confirmed on both halves: the dedicated test has teeth, and the timing-based test
has none for this defect. `pool.go` was then restored and verified
**byte-identical** against the backup, with zero mutation markers remaining, and
the suite re-run clean.

This is the strongest evidence produced anywhere in the run so far. SPEC §26 is
the one requirement the completion gate cannot falsify, and there is now a test
that fails deterministically when the invariant is broken. The invariant comment
in `pool.go` names that test so a later reader cannot quietly delete the read.

Note the worker also reported that **only the timing assertion caught the
mutation — the state assertions stayed green**. That is precisely why §26 is a
stronger requirement than state consistency, and it is worth remembering when
reviewing T8's race tests.

**Two defects the worker found via its own tests, not by inspection:** a job
signalled before execution began returned without recording anything, stranding
the row in `RUNNING` (SPEC §35 step 4 violation); and a `defer` ordering bug let
`Wait()` return while a worker was still counted live, which surfaced only at
`-count=10`.

**Criteria spot-checked by the orchestrator:** parallel execution asserts four
100ms delays finish in under the 400ms serial baseline (real concurrency, not a
sleep); shutdown asserts `live == 0` after `Wait` and `spawned == count`, which
also proves no goroutine-per-job growth (SPEC §32).

**SPECIFICATION DEVIATION — `EXECUTION_ERROR`.** The worker introduced a job
error code that SPEC does not define, for an executor error that is neither a
`*jobs.JobError` nor a cancellation. Payloads are validated before a job is
persisted, so the branch is defensive and unreachable in normal operation; it
exists so an unexpected error moves the job out of `RUNNING` rather than
stranding it until the next startup recovery. SPEC §23 requires persisting an
execution error on `RUNNING → FAILED` but enumerates no code for this case, and
`internal/jobs` was off-limits to this task, so the constant lives in
`internal/worker`.

Judged acceptable: it fills a genuine gap, cannot leak database internals (the
three executors never touch the store), and the alternative is a stranded row.
**Action required in T9:** SPEC §2 requires materially important design decisions
to be documented in the README — this one, the extra `SELECT` per claimed job,
and the registration-ordering invariant all qualify.

**Contract handed forward to T6/T7** (from the worker, to be verified when those
tasks run): `main` constructs `worker.NewRegistry()` and passes it to both pool
and API; the cancel handler order is `store.Cancel` → `registry.Cancel(id)`, with
a `false` return being normal rather than an error; shutdown order is
`StopClaiming()` → `http.Server.Shutdown` → `CancelRunning()` → `Wait()`.

### T5 — HTTP foundation (`internal/api`)

- Initial profile: `worker-sonnet`
- Status: **COMPLETE**, 1 dispatch. 34 tool uses, ~221s, ~65k tokens, 22 tests.

Orchestrator-run verification:

```
$ test -z "$(gofmt -l .)" && go build ./... && go vet ./... && go test -race ./internal/api/ -count=1
ok  	taskforge/internal/api	1.288s
EXIT=0

$ go test ./...
ok  	taskforge/internal/api	0.169s
ok  	taskforge/internal/jobs	(cached)
ok  	taskforge/internal/store	(cached)
ok  	taskforge/internal/worker	(cached)
```

**The ServeMux trap was avoided.** The planning-phase probe found that
method-qualified patterns plus a `/` catch-all silently disable the built-in 405
— `DELETE /jobs` returned 404 while looking correct. The dispatch led with that
probe output and required a specific test as proof. The worker used path-only
patterns with an internal method switch, and
`TestRouting_DeleteJobsMethodNotAllowed` asserts 405 with a non-empty `Allow`.

Handing a worker the *verified failure output* of a trap, rather than a warning
about it, is now the third case where that has worked (after the `json.Number`
finding in T1 and the `RowsAffected` primitive in T3).

**Other substance checks:** the health 503 test greps the body for `sqlite`,
`.db`, `sql:` and `no such` — a real leak check rather than a status assertion.
`Content-Type: application/json` is asserted on error responses. Scope held: no
`store.List/Cancel/Retry/Claim` wired in, confirmed by grep.

**Carried into T6:** `/jobs` emits `Allow: POST`, correct while only POST exists,
but it must widen to `GET, POST` when list lands. Included in T6's dispatch with a
test required.

### T6 — List, cancel, and retry endpoints (`internal/api`)

- Initial profile: `worker-sonnet` → **escalated to `escalation-opus`**
- Status: **COMPLETE** via escalation. Package now has 47 passing tests.

**Attempt 1 — `worker-sonnet` — FAILED (turn exhaustion).** 40 tool uses, ~202s,
~81k tokens. Cut off mid-edit; last words *"Need to add `strings` import."*

Notably, **the recalibrated 40-turn budget did not eliminate exhaustion — it moved
where exhaustion happens.** 40 turns sufficed for T2, T3, T4 and T5 and not for
T6. Useful calibration data: the budget is now adequate for most tasks in this
plan but not universally.

State left behind: production code (`list.go`, `cancel.go`, `retry.go`,
`router.go`) built cleanly; only test files were broken.

```
$ gofmt -l .
internal/api/list_test.go
$ go vet ./...
vet: internal/api/list_test.go:107:46: undefined: jobs.NewJobError
     internal/api/list_test.go:157:50: undefined: jobs.NewJobError
```

**Escalation decision.** This was genuine exhaustion, which is precisely the
`CLAUDE.md` ladder's trigger, so the next bounded attempt went to
`escalation-opus`. A cheap narrow Sonnet repair would very likely have fixed the
two-line compile error, and the orchestrator noted at dispatch time that the
escalation looked **disproportionate to the visible defect** — but substituting a
tier on the orchestrator's own judgement is exactly the policy drift this
apparatus exists to detect, so the ladder was followed and the observation
recorded instead.

**That judgement turned out to be wrong, and the policy right.** The visible
defect was not the real one.

**Attempt 2 — `escalation-opus` — RESOLVED.** 40 tool uses, ~456s, ~103k tokens.

Its diagnosis: `go vet` reports only the first failure, so the compile error
**masked** everything behind it. Once the package compiled, two further problems
surfaced — a test that assumed `Claim` would return the *second* job created,
contradicting SPEC §20's `queued_at ASC` ordering; and, far more seriously,
**acceptance criteria 2–5 had no tests at all.** There was no `cancel_test.go` and
no `retry_test.go`; `cancel.go` and `retry.go` were entirely unexercised. The
first attempt had spent its budget on the list endpoint alone.

**This is the run's clearest lesson about the gate.** Had the compile error been
patched in isolation — the "obvious" cheap fix — the suite would have gone green
with two handlers completely untested, and both the gate and a casual substance
check would have passed it. The escalation was disproportionate to the *visible*
defect and proportionate to the *actual* one.

Opus kept all production code byte-identical and added the missing coverage,
including both directions of the SPEC §26 cancel-ordering contract.

**Anti-vacuity: it mutation-tested each new assertion** (mutations reverted),
reporting that each of these is caught by a named test: signalling the registry
before the store transition; already-`CANCELLED` returning 409 instead of an
idempotent 200; `ATTEMPT_LIMIT_REACHED` collapsed into
`INVALID_STATE_TRANSITION`; `Allow: POST` only; check-then-act retry (3 winners
instead of 1); and `ORDER BY created_at DESC` without `id ASC`.

**Orchestrator verification.** Cold cache, whole repo:

```
gofmt clean; build ok; vet ok
$ go test -race ./...
ok  	taskforge/internal/api	1.787s
ok  	taskforge/internal/jobs	1.136s
ok  	taskforge/internal/store	1.510s
ok  	taskforge/internal/worker	4.237s
real	0m4.539s
```

Contract checks by inspection: `cancel.go` calls `h.store.Cancel` first and
signals `h.registry.Cancel(id)` only in the `OutcomeWon` branch; neither cancel
nor retry pre-reads a job (`grep` for `h.store.Get` in both files returns
nothing), so no check-then-act was introduced.

**One entry of the anti-vacuity table verified independently** rather than
trusting the whole table: mutating line 74 of `router.go` from
`MethodGet+", "+MethodPost` to `MethodPost` produced

```
--- FAIL: TestRouting_JobsAllowsGetAndPost (0.02s)
    api_test.go:483: Allow = "POST", want it to name both GET and POST
```

`router.go` restored byte-identical and the suite re-verified.

**Cost of T6:** 2 dispatches, ~184k subagent tokens, ~11 min worker wall clock,
1 escalation, 0 human escalations.

### T7 — Composition root (`main.go`, `app.go`, `config.go`)

- Initial profile: `worker-sonnet`
- Status: **COMPLETE**. 1 dispatch, exhausted at 40 turns — but exhausted while
  adding an *extra* test, with all work already done. No escalation needed.

**Turn exhaustion, second occurrence at 40.** 40 tool uses, ~1639s (~27 min),
~90k tokens. Cut off mid-sentence: *"Now let's write the binary integration test
… and a subprocess."* Unlike T6, the cutoff landed after the deliverable was
complete: `gofmt`, `go build` and `go vet` were all already clean, and
`main_binary_test.go` existed and passed.

**The worker filed no report, so this task was judged purely on the artifact.**

Orchestrator-run verification, cold cache, whole repo:

```
gofmt clean; build ok; vet ok
$ go test -race ./... -count=1
ok  	taskforge	1.604s
ok  	taskforge/internal/api	2.235s
ok  	taskforge/internal/jobs	1.138s
ok  	taskforge/internal/store	1.909s
ok  	taskforge/internal/worker	4.669s
```

12 tests in package `main`, covering all five acceptance criteria.

**Structural checks (stronger than the tests, because they cannot drift):**

- **SPEC §34 ordering is structural, not incidental.** `newApp` performs
  `store.Open` → `Recover` during *construction*; `Start()` only afterwards calls
  `Serve` and `pool.Start()`. Neither the listener nor a worker can observe a
  stale `RUNNING` row, because recovery finishes before the object exists.
- **`os.LookupEnv`, not `os.Getenv`**, for `DATABASE_PATH`, with a comment
  explaining that `Getenv` cannot distinguish unset from set-to-empty. This was
  the subtlest config requirement in SPEC §4 and it was handled correctly.
- `main.go` keeps `signal.NotifyContext` as a thin wrapper over the
  deterministic `Start`/`Shutdown` pair, satisfying SPEC §42 by construction.

**Live end-to-end smoke test run by the orchestrator against the real binary**
(`PORT=18080`, `WORKER_COUNT=2`, temp `DATABASE_PATH`) — not a unit test:

| Check | Result |
|---|---|
| `GET /health` | `200 {"status":"ok"}` |
| `POST /jobs` hash | 201, `QUEUED`, `attempt_count 0`, exact SPEC §11 shape |
| after execution | `COMPLETED`, `sha256` = `b94d27b9…efcde9`, `created ≤ queued ≤ started ≤ finished` |
| fail job | `FAILED` / `INTENTIONAL_FAILURE`, `attempt_count 1` |
| retry | 200, re-queued, re-ran, `attempt_count 2` (retry itself does not increment) |
| cancel a running 5000ms delay | **200 in 23ms**, `CANCELLED`, `result` and `error` both null |
| `?status=BOGUS` / `?type=nope` | 400 `INVALID_FILTER` |
| `?foo=bar` | 200 — unknown parameter *names* ignored, as assumed |
| `/jobs/not-a-uuid` | 404 `JOB_NOT_FOUND` |
| `/nope` | 404 `ROUTE_NOT_FOUND` |
| `DELETE /jobs` | 405 `METHOD_NOT_ALLOWED`, `Allow: GET, POST` |
| `text/plain` body | 415 `UNSUPPORTED_MEDIA_TYPE` |
| unknown field / trailing data | 400 `INVALID_JSON` |
| `milliseconds: 99` | 400 `INVALID_PAYLOAD` |
| SIGTERM | exit code 0, `server shutting down` logged, **0 rows left `RUNNING`** |

The 23ms cancellation of a 5-second delay is SPEC §26 working in the shipped
binary, not just in tests.

One log line from the smoke run is worth preserving, because it shows the
cancellation race resolving correctly in production rather than in a fixture:

```
INFO job failure skipped; another transition won
     job_id=30a67c13… job_type=delay attempt_count=1 status=CANCELLED
```

The worker's shutdown-failure update found the row already `CANCELLED` and
declined to overwrite it — exactly SPEC §25.

**SPEC §36 lifecycle events confirmed in real output**, all logged
unconditionally, and their order is itself evidence for §34:

```
INFO startup recovery completed jobs_recovered=0
INFO worker pool started worker_count=2
INFO server started addr=[::]:18080
...
INFO server shutting down
```

(SPEC §34 requires recovery before *both* the listener and the workers; it does
not order the listener against the workers, so starting workers first is
compliant.)

**Not covered by the smoke run:** no job was in flight at shutdown — the delays
had already completed — so the live run did not exercise the `SERVER_SHUTDOWN`
path. That path is covered by
`TestGracefulShutdownInterruptsRunningLeavesQueuedRecordsServerShutdown`, which
asserts the error code, that queued jobs stay `QUEUED`, and that shutdown returns
well inside its bound. Recorded here so the smoke table is not read as covering
more than it does.

**Orchestrator process note.** Several smoke-test commands failed on my own
mistakes rather than the code: `go run .` from the scratchpad (no module), the
binary invoked with `--help` (it takes no flags, so it started a second server
and blocked), and a `/proc` scan whose grep pattern matched the scanning shell
itself and killed it (exit 144). None touched the repository, and the stray
processes and databases were cleaned up; `/work` has no stray `*.db`.

### T8 — Cross-cutting concurrency and race tests

- Initial profile: `worker-sonnet`
- Status: **COMPLETE**, 1 dispatch. 43 tool uses, ~560s, ~108k tokens, 423 lines
  in `internal/api/race_test.go`. Test-only; no production file touched.

**Scope was cut before dispatch.** The orchestrator inventoried existing coverage
first and found five of SPEC §40's seven items already tested. Only
cancellation-vs-completion and cancellation-vs-failure were genuinely missing as
*forced races* (a sequential `TestComplete_DoesNotOverwriteACancelledJob` existed,
which is not a race), plus an end-to-end attempt-count check. Telling the worker
exactly what not to rebuild is likely why this task fit in budget where T6 and T7
did not.

**T4's lesson was passed forward as a design constraint**, not a footnote: when
the §26 invariant was mutated, every state assertion stayed green and only a
timing assertion caught it. So the dispatch required assertions on *agreement*
between the API response and the persisted state, re-read after the pool drains.

The delivered test does exactly that:

```go
// Drain the pool before re-reading final state: a losing worker's
// completion may still be in flight when the cancel response was
// written, and that is exactly the window a broken guard would show up in.
pool.Stop()
```

200 ⇒ `CANCELLED` post-drain with `result` and `error` both null; 409 ⇒
`COMPLETED` unchanged with non-null `result` and null `error`; any other status
is an error. The SPEC §11 shape check means a losing transition that partially
wrote fields would be caught even if the status looked right.

**Splits verified by the orchestrator across three independent runs** — the
anti-vacuity check that matters, since a one-sided run would prove nothing:

```
cancellation vs completion: 41/50 cancel won, 9/50 completion won
cancellation vs completion: 40/50 cancel won, 10/50 completion won
cancellation vs completion: 40/50 cancel won, 10/50 completion won
cancellation vs failure:    13/50 cancel won, 37/50 failure won
cancellation vs failure:     7/50 cancel won, 43/50 failure won
cancellation vs failure:    10/50 cancel won, 40/50 failure won
attempt count: 72 execution attempts observed, 0 double-increments  (x3)
```

Both branches occurred in every run. The test logs the split and emits a visible
note if a run comes out one-sided, so a future vacuous pass is legible rather
than silent.

### T9 — README

- Initial profile: `worker-sonnet`
- Status: **COMPLETE**, 1 dispatch. 19 tool uses, ~91s, ~58k tokens, 389 lines.
  Ran in parallel with T8 — the one genuinely disjoint pair (test files vs. a
  Markdown file), as the plan anticipated.

All 20 SPEC §48 items present, with an ASCII architecture diagram showing the
HTTP layer, worker pool, shared cancellation registry, store and SQLite file.

Two things exceeded the brief. The architecture prose names the actual invariant
rather than restating the structure: the registry *"is never consulted to decide
whether a transition is allowed — the store's guarded `UPDATE` is what decides
that."* And `EXECUTION_ERROR` has its own section headed **"an addition beyond
the specification"**, documenting it as an addition rather than blending it into
the specified code list.

**Closure ordering.** T9 was verified but deliberately held open until T8
returned, then closed first. Closing fires the repo-global gate; had T8 been
mid-edit, T9 would have been refused for reasons unrelated to T9. This is the
global-gate coupling the plan cited as the reason to prefer sequential execution.

---

## Definition of Done evidence

All commands below were run by the orchestrator, on a cold test cache, after T9.

### SPEC §44 required verification

```
$ go build ./...                 PASS
$ go vet ./...                   PASS
$ gofmt -l .                     PASS (no output)
$ go test ./...
ok  taskforge 0.468s | internal/api 5.489s | internal/jobs 0.122s
   | internal/store 0.544s | internal/worker 2.102s
$ go test -race ./...
ok  taskforge 1.575s | internal/api 9.285s | internal/jobs 1.132s
   | internal/store 1.922s | internal/worker 4.538s
```

**135 passing tests** across five packages. Whole suite ~5.5s unraced, ~9.3s
raced, against the gate's 300s budget.

### SPEC §49 checklist

| Item | Evidence |
|---|---|
| build / test / race / vet / gofmt | the five commands above |
| REST API matches contract | live smoke test of all six endpoints against the real binary (T7 notes) |
| JSON validation rules | live: 415, `INVALID_JSON` (unknown field, trailing data), `INVALID_PAYLOAD` (ms=99); 22+ unit tests |
| three job types behave as specified | live hash digest `b94d27b9…efcde9`; delay cancelled in 23ms; fail → `INTENTIONAL_FAILURE` |
| transitions atomic | every transition a guarded `UPDATE`; SQL audited by orchestrator, no read-then-write |
| multiple jobs concurrent | `TestIndependentDelayJobsRunConcurrently` (4×100ms under 400ms serial) |
| one job never concurrent | `TestSingleQueuedJobRunsExactlyOnce`, `TestEveryJobIsClaimedExactlyOnce` |
| claiming concurrency-safe | `TestClaim_ExactlyOneWinnerAndSingleIncrement`, 8 racing goroutines |
| `attempt_count` concurrency-safe | store-level + end-to-end: 72 attempts, 0 double-increments ×3 runs |
| queue ordering deterministic | `TestClaim_OrdersByQueuedAtThenIDTiebreak` with a constructed tie |
| cancellation idempotent | `TestCancelJob_AlreadyCancelled` (asserts `updated_at` does not move) |
| successful cancel ⇒ final CANCELLED | `TestRace_CancellationVsCompletion`, re-read post-drain |
| cancel/completion race | 40/10 split ×3 runs, agreement asserted |
| cancel/failure race | 13/37, 7/43, 10/40 splits, agreement asserted |
| delay responds promptly | live: 23ms on a 5000ms delay; `TestCancelRunningJobReturnsPromptly` |
| retries concurrency-safe | `TestRetryJob_ConcurrentRetries` — exactly one 200 |
| max attempts enforced | `TestClaim_NeverClaimsAtAttemptLimit`, `TestRetry_AttemptLimitReached` |
| retry resets fields | `TestRetry_ClearsFieldsAndRefreshesQueuedAt` |
| jobs survive restart | `TestCreateGet_RoundTrips*` across close/reopen |
| startup recovery | `TestStartupRecoveryPreservesAttemptCountAndSetsFinishedAt`; live log `startup recovery completed` before `server started` |
| graceful shutdown | live SIGTERM → exit 0, `server shutting down`, 0 rows left `RUNNING`; `TestGracefulShutdown*` |
| no race findings | `go test -race ./...` clean, repeatedly |
| README documents operation | 389 lines, all 20 SPEC §48 items |

## Vacuous gate passes

**None in the final state**, but two were avoided by design and one near-miss was
caught in review:

1. **A standalone module-bootstrap task would have been vacuous** — planning found
   `go test ./...` exits 0 for a package with no test files. The bootstrap was
   folded into T1 so the first closable task had real tests. (It also exits **1**
   with go.mod but *no* packages, which is why a bootstrap-only task could not
   have closed at all.)
2. **T2's gate passed on a suite that did not test what SPEC §39 requires** — the
   populated field shape was never read back across a database reopen. Caught by
   substance review, not by the gate, and sent back.
3. **T6's compile error masked two entirely untested handlers.** `go vet` reports
   only the first failure; patching it in isolation would have produced a green
   suite with `cancel.go` and `retry.go` unexercised.

`t.Skip` audit: one occurrence, `main_binary_test.go:26`, the idiomatic
`testing.Short()` guard on a subprocess test. Verified it **runs and passes**
under the gate's actual command (`go test ./...`, no `-short`). Not a weakened
test.

## Apparatus integrity

- `git diff main..HEAD -- .claude/` shows **only** `test-command.conf`, as the
  experiment design requires.
- `.claude/hooks/verify-unit-tests.sh` is **byte-identical** to main's (verified
  by `diff`), so this arm ran the same gate as every other.
- **No implementation task modified anything under `.claude/`.** The only
  uncommitted changes there are orchestrator/human actions: the three
  `agents/*.md` turn budgets (the user's chosen resolution to T1's escalation),
  `escalations/1.md` (required by policy), and `metrics/task-events.jsonl`
  (written by the harness's own hook).
- `go.mod` still reads `go 1.23.5`; no toolchain was installed or switched at any
  point.
- Exactly one direct dependency, `modernc.org/sqlite v1.39.0`, as specified.

**Caveat for cross-arm comparison, repeated here because it is easy to miss:**
the turn budgets were raised mid-run (haiku 8→15, sonnet 15→40, opus 15→50). This
arm is therefore **not comparable** to the warmup arms without re-running them at
the new budgets. `check-arm-ready.sh` guards `hooks/` but not `agents/`, so it
will not flag this divergence — its silence is not evidence of a shared
apparatus.

## Specification deviations

**One.** `EXECUTION_ERROR`, a job error code SPEC does not define, introduced in
T4 for an executor error that is neither a `*jobs.JobError` nor a cancellation.
Payloads are validated before persistence, so the branch is unreachable in normal
operation; without it such a job would remain `RUNNING` until the next startup
recovery. SPEC §23 requires persisting *an* execution error but enumerates no
code for this case. Accepted by the orchestrator, flagged to the user, and
documented in `README.md` under a heading that names it as an addition beyond the
specification.

No requirement was left unsatisfied, and no check was skipped, disabled,
weakened, or ignored.
