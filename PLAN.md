# TaskForge — Implementation Plan

Planning output for `SPEC.md`, produced under `.claude/TASK_PLANNING_INSTRUCTIONS.md`.
Reviewed and approved. **Ready to execute.**

`SPEC.md` remains the authoritative statement of required behavior. This file is
the execution plan: task decomposition, worker assignment, verification, and the
environment facts already established so they are not rediscovered.

---

# 0. How to execute this plan

> Say **“execute @PLAN.md”** and follow this section top to bottom.

## 0.1 Your role

You are the **orchestrator**. Read `CLAUDE.md` first — its agent execution safety
policy governs everything below and overrides any instinct to move faster.

The parts that most often get violated:

- **You own the entire task lifecycle.** You call `TaskCreate`, you set
  `in_progress`, you dispatch exactly one worker, and after that worker returns
  you close the task. Workers do not have `TaskCreate`/`TaskUpdate` and must not
  be asked to use them. A worker reporting it lacks them is describing the
  expected state, not a blocker.
- **You run the verification yourself** after a worker returns. Not the worker's
  claim that tests pass — your own command output. Closing a task fires the
  `TaskCompleted` gate, and the gate must fire where the evidence is.
- **You do not implement.** Every code change goes through a dispatched worker.
- The gate is a **necessary, not sufficient** condition. A passing suite means
  nothing was detected that blocks completion. Close a task only when its
  acceptance criteria are satisfied *in substance* as well.

If you do not have `TaskCreate`, `CLAUDE_CODE_ENABLE_TODO_TOOLS=true` is missing
from the session. **Stop.** Do not fall back to `TodoWrite` and do not run
workers untracked — both silently erase what this repository measures. Start a
session that has the tools.

## 0.2 Pre-flight (before creating T1)

```bash
git rev-parse --abbrev-ref HEAD          # expect experiment/go-job-processing-service
git status --porcelain .claude/          # expect EMPTY — see below
cat .claude/hooks/test-command.conf | grep '^TEST_COMMAND'   # expect "go test ./..."
go version                               # expect go1.23.5; do NOT change it
```

1. **`.claude/hooks/test-command.conf` must be committed.** At planning time it
   was modified-but-uncommitted. `check-arm-ready.sh` treats an uncommitted gate
   config as a tripwire finding, and no implementation task may touch `.claude/`.
   Commit it before dispatching anything.
2. Confirm the `SessionStart` tripwire printed no warnings. If it warned that
   `OTEL_RESOURCE_ATTRIBUTES` is unset, this session was not launched through
   `.claude/sandbox/run-arm.sh` and its cost/token metrics cannot be attributed
   to this arm. Decide deliberately whether to continue.
3. Create `EXECUTION_NOTES.md` now and append to it as you go (§0.7). Writing it
   at the end from memory is how the details worth recording get lost.

## 0.3 The per-task loop

For each task, in the order given by §4:

1. `TaskCreate` with the task's objective and acceptance criteria.
2. `TaskUpdate` → `in_progress`.
3. Dispatch **one** worker of the assigned profile, using the template in §0.4.
4. Receive the worker's report. Read it; do not act on it yet.
5. **Run the task's verification command yourself.** Also run
   `git diff --stat main..HEAD -- .claude/` and confirm it is empty apart from
   `test-command.conf`, and `grep '^go ' go.mod` and confirm it still reads
   `go 1.23.5`.
6. Read the diff the worker actually produced and check the acceptance criteria
   against it — the gate cannot see substance.
7. If criteria are met and verification passes → `TaskUpdate` → `completed`
   (this fires the gate). If the gate refuses, the task stays open; go to §0.5.
8. Append the outcome to `EXECUTION_NOTES.md`.

Do not batch steps 5–7 across tasks. One task closes before the next opens.

## 0.4 Dispatch prompt template

**Deliverable first, verification last, verification batched into one command.**

This ordering is not cosmetic. On an earlier run a `worker-haiku` task exhausted
all 8 of its turns running verification commands and returned having never
created the file it was assigned — the prompt had listed the checks first, and a
checklist reads as an instruction to execute it.

### Lead with a concrete first action — learned from T1

T1 took four dispatches. The decisive variable was **prompt scope, not model
tier**: Opus at the same budget did no better than Sonnet.

| Attempt | Worker | Instruction shape | Outcome |
|---|---|---|---|
| 1 | sonnet | understand, then build | cut off; nothing delivered |
| 2 | opus | understand, then build | cut off; 1208 lines, did not compile |
| 3 | sonnet | read and verify, then fix | cut off; identified the fix, could not apply it |
| 4 | sonnet | **apply this edit first, then iterate** | success in 5 tool uses, 14s |

Every failed attempt was told to comprehend before acting, and spent its budget
comprehending. The successful one named a specific first edit and let test output
drive everything after it.

So, when composing a dispatch:

- **Name the first concrete action** and put it first. "Create
  `internal/store/schema.go` with …" or "In `job_test.go:192`, replace X with Y".
  Never open with "read the package and understand it".
- **Let verification drive discovery.** "Run the command, then fix what it
  reports, and repeat" costs one turn per real defect. "Review the code for
  correctness" costs the whole budget and finds less.
- **Supply resolved design decisions rather than questions.** If a task hinges on
  a detail a worker would have to investigate, investigate it yourself first and
  hand over the verified answer. Attempt 1 died investigating whether
  `json.Number` was needed; a two-minute orchestrator probe settled it, and every
  later attempt used the result for free.
- **Scope reading explicitly.** "Read only the file a failure points at" is worth
  saying out loud. Existing code a worker must not re-derive should be described
  in the prompt (paths, line counts, what each file holds) rather than discovered.

This matters most for T3, T5 and T7, which are the largest remaining tasks.

```
<Objective — one sentence, from the task below>

<Scope — the files/packages you may touch>

<Implementation guidance — verbatim from the task>

<Acceptance criteria — verbatim from the task>

Constraints:
- Do not modify anything under .claude/. Do not modify SPEC.md, PLAN.md, or
  EXECUTION_NOTES.md.
- Do not change the `go 1.23.5` directive in go.mod, do not set GOTOOLCHAIN,
  and do not install or switch Go toolchains.
- Never run `go get modernc.org/sqlite` unpinned — see the version note in the
  guidance.
- Do not weaken, skip, or delete tests to make anything pass.
- Do not add functionality beyond this task's scope.

When you are done, run exactly this and report its real output:

    <the task's single batched verification command>

Then report: files changed, which acceptance criteria are satisfied, which are
not, and RESULT: success | failure.
```

## 0.5 Escalation

Per `CLAUDE.md`, one bounded attempt per tier, no retries, no extra tiers:

```
worker-sonnet  → failure/exhaustion → escalation-opus → failure → STOP / human
escalation-opus (direct, T4 only) → failure → STOP / human
```

**Escalation handoff.** Workers start with no memory of prior attempts. When you
dispatch `escalation-opus` you must pass forward: the original task and criteria,
what the previous worker changed (by path, from the diff — not from its report),
every approach it tried, why each failed, and the **actual verification output**
you observed. Without this, Opus's single attempt begins uninformed and repeats
the same approach.

**On STOP.** Write `.claude/escalations/<task-id>.md` from
`.claude/escalations/TEMPLATE.md` — you are the only agent holding the full
attempt history. Then halt that task's dependents (§4 lists them) and stop.
Writing an escalation record is the one exception to "no implementation task
touches `.claude/`"; it is orchestrator bookkeeping, not implementation.

## 0.6 Standing constraints

Repeat these in every dispatch (they are in the §0.4 template):

- `.claude/` is harness. No task may modify it.
- Repository root belongs to the application. `README.md` goes at the root.
- Pin `modernc.org/sqlite` to **v1.39.0**. Never `go get` it unpinned.
- Keep `go 1.23.5`. Never install or switch toolchains.
- No new direct dependencies beyond `modernc.org/sqlite`.
- No functionality beyond the specification (`SPEC.md` §22, §50).

## 0.7 `EXECUTION_NOTES.md`

Required by `.claude/TASK_PLANNING_INSTRUCTIONS.md` §13. Write it as you go, not
at the end. It has repeatedly caught what no gate did. Include:

- every task, its initial profile, and its outcome (success / escalation / stop);
- for any escalation: what failed, your diagnosis, what resolved it;
- deterministic evidence for each Definition-of-Done item, **from commands you
  ran yourself**, not from a worker's report;
- **any vacuous gate pass** — the suite ran but proved nothing. Watch for this
  specifically: `go test ./...` exits 0 when packages exist with no test files,
  and the gate runs without `-count=1` so it can serve a cached pass;
- any container/toolchain change made during the run (these do not persist and
  make the arm non-reproducible);
- apparatus integrity: confirmation that no task modified anything under
  `.claude/`;
- any specification deviation, with justification.

**Already known, carry it forward:** planning warmed the shared Go module and
build caches at `/home/agent/go/pkg/mod` by probing `modernc.org/sqlite`. That
does not survive the container, so a cold reproduction pays a one-time download.
No toolchain was installed or switched; nothing under `/work` was modified.

---

# 1. Verified environment facts

Established during planning by direct probe. **Do not re-derive these.**

| Fact | Detail |
|---|---|
| Toolchain | `go1.23.5 linux/arm64`; `gofmt` and `gcc` present; `GOFLAGS=-mod=mod`; module proxy reachable |
| **SQLite driver version** | Latest `modernc.org/sqlite` (v1.56.0) requires **`go >= 1.25.0`**. An unpinned `go get` rewrites the go directive to `1.25.0`, then refuses to build under `GOTOOLCHAIN=local` — a toolchain switch, forbidden by SPEC §3. **v1.39.0 is the newest release with a `go 1.23.0` directive.** Pinned, tidied, built and ran clean; cold build 5.4s; directive stayed `1.23.5` |
| `go test -race` | Works. Needs cgo for the race runtime; gcc is present. SPEC §3's "no CGO" constrains the application and driver (both pure Go), not the race runtime |
| **Atomic conditional UPDATE** | 8 goroutines racing `UPDATE … WHERE id=? AND status=?` under `SetMaxOpenConns(1)` → exactly one `RowsAffected`. This is the primitive the whole spec rests on, and it is verified in *this* environment |
| `UPDATE … RETURNING` | Supported (SQLite 3.50.3). `UPDATE … WHERE id=(SELECT … ORDER BY … LIMIT 1) RETURNING id` works; returns `sql.ErrNoRows` when nothing matched |
| **`http.ServeMux` 405 trap** | Registering a `"/"` catch-all **suppresses ServeMux's built-in 405**. In a probe, `DELETE /jobs` fell through to `/` and returned 404 — silently violating SPEC §31. See T5 |
| **Gate vacuity boundaries** | `go test ./...` with go.mod but **no packages** exits **1** (gate refuses). With a package and no test files it exits **0** (vacuous pass). This is why module bootstrap is folded into T1 rather than standing alone |

---

# 2. Design record (SPEC §51)

**1. Package structure** — module `taskforge`, `go 1.23.5`:

```
go.mod  go.sum  main.go  README.md
internal/jobs/    domain: Job, Status, Type, transition table, payload
                  validation, hash/delay/fail executors, job-error codes, UUIDv4
internal/store/   SQLite: schema, CRUD, atomic conditional transitions,
                  list/filter/order, recovery, ping
internal/worker/  pool, claim loop, running-job cancellation registry
internal/api/     router, error envelope, handlers
```

Acyclic: `main → api → {store, worker, jobs}`, `worker → {store, jobs}`,
`store → jobs`.

**2. Persistence** — one `jobs` table; `payload`/`result`/`error` as JSON `TEXT`;
`SetMaxOpenConns(1)`; `CREATE TABLE IF NOT EXISTS` at startup. Timestamps stored
as **fixed-width** UTC strings, layout `2006-01-02T15:04:05.000000000Z07:00`.

This matters: `time.RFC3339Nano` strips trailing zeros, so `".1Z" > ".15Z"`
lexicographically — it would corrupt both the worker queue order and the list
order. Fixed width makes string order identical to chronological order.

**3. Atomic transitions** — *every* transition is one guarded statement,
`UPDATE jobs SET … WHERE id=? AND status=?` (plus `attempt_count < 3` where
required); `RowsAffected()==1` means "this caller won". No read-then-write
anywhere (SPEC §15).

**4. Worker claiming** — poll for
`status='QUEUED' AND attempt_count<3 ORDER BY queued_at ASC, id ASC LIMIT 1`,
claimed by a single guarded `UPDATE` that simultaneously sets `RUNNING`,
increments `attempt_count`, and sets `started_at`/`updated_at`. `RETURNING` is
available if a single statement is preferred. Fixed pool of `WORKER_COUNT`
goroutines; a lost claim just re-polls.

**5. Running-job cancellation** — a mutex-guarded `map[jobID]context.CancelFunc`,
owned by `main`, shared by the pool and the API. Two ordering rules close the
SPEC §26 window:

- the worker **registers before** attempting the claim `UPDATE` (and unregisters
  if the claim is lost);
- the API **signals only after** it has won the DB transition to `CANCELLED`.

So if cancel wins while the job is `QUEUED`, the worker's claim finds 0 rows and
it never runs; if the claim wins first, the registry entry already exists when
the signal arrives. There is no interleaving where `CANCELLED` is persisted and
the worker still begins meaningful execution.

---

# 3. Tasks

## Task T1: Domain model, job execution, and module initialization

**Objective** — A self-contained `internal/jobs` package holding the job domain
and the three executors, plus a valid Go module.

**Scope** — `go.mod`, `.gitignore`, `internal/jobs/*.go` + tests. No SQLite, no
HTTP.

**Dependencies** — None.
**Parallelizable** — No (everything depends on it).
**Complexity** medium · **Ambiguity** low · **Risk** medium
**Initial execution profile** — `worker-sonnet`

**Profile rationale** — Ordinary domain modelling and table-driven tests; no
concurrency, no I/O.

**Implementation guidance**

- `go mod init taskforge`; keep the directive at `go 1.23.5`. **Do not** add
  SQLite here (T2 owns it) and **do not** run `go get modernc.org/sqlite` —
  unpinned it resolves to v1.56.0, which demands go1.25 and triggers a forbidden
  toolchain switch.
- Add `.gitignore` entries for the database file and built binary
  (`/taskforge.db`, `*.db-wal`, `*.db-shm`, `/taskforge`).
- `Job` marshals to exactly the 12 fields in SPEC §11, in that shape.
  `payload`/`result`/`error` are `json.RawMessage`; nullable timestamps are
  `*time.Time` rendering as JSON `null`.
- Export the fixed-width timestamp layout helper from §2 — the store and the API
  both depend on it.
- Transition validity is a pure function over the SPEC §12 table. The only legal
  edges: `QUEUED→RUNNING|CANCELLED`, `RUNNING→COMPLETED|FAILED|CANCELLED`,
  `FAILED→QUEUED`.
- UUIDv4 from `crypto/rand` in ~10 lines — SPEC §47 discourages new direct
  dependencies and the stdlib suffices. No UUID *parsing* is needed anywhere:
  SPEC §18 makes a malformed id a plain 404, so it is just a lookup miss.
- Strict payload validation: `json.Decoder` with `DisallowUnknownFields`, plus an
  explicit "exactly one field" check per type. `hash` → `text` string (empty
  valid); `delay` → integer `milliseconds` in `[100,30000]` (reject non-integers
  like `100.5`); `fail` → empty object.
- Executors take `context.Context`. `delay` must return promptly on cancellation
  (`select` on `ctx.Done()` and a timer), not sleep out the duration.
- Define the three **job**-level error codes here: `INTENTIONAL_FAILURE`,
  `INTERRUPTED_EXECUTION`, `SERVER_SHUTDOWN`. These are a different namespace
  from the API error codes of SPEC §30 — do not merge them.

**Acceptance criteria**

- Every legal transition is accepted and every illegal one rejected, including
  the terminality of `COMPLETED` and `CANCELLED`.
- `hash` over `"hello world"` yields
  `b94d27b9934d3e08a52e52d7da7dabfac484efe37a5380ee9088f7ace2efcde9`, lowercase
  hex; the empty string succeeds.
- A `delay` execution cancelled 20ms into a 30000ms delay returns in well under a
  second with a cancellation error.
- `fail` always returns the specified `INTENTIONAL_FAILURE` error.
- Payload validation rejects unknown fields, missing fields, wrong types, and
  out-of-range `milliseconds`; accepts the SPEC §7–§9 examples.
- Marshalling a job produces exactly the SPEC §11 field set, with `null` for
  absent values.

**Task-specific verification**

```bash
test -z "$(gofmt -l .)" && go build ./... && go vet ./... && go test -race ./internal/jobs/ -count=1
```

**Completion condition** — the command above passes, the acceptance criteria are
demonstrably satisfied in the diff, and `go.mod` still reads `go 1.23.5`.

**Escalation** — `worker-sonnet` → `escalation-opus` → STOP.
**Downstream if stopped** — everything.

---

## Task T2: Persistence foundation

**Objective** — `internal/store` opens and initializes the database and supports
create, get, filtered list, and a health ping, with values surviving reopen.

**Scope** — `internal/store/*.go` + tests; adds the SQLite dependency to
`go.mod`/`go.sum`.

**Dependencies** — T1.
**Parallelizable** — No.
**Complexity** medium · **Ambiguity** low · **Risk** medium
**Initial execution profile** — `worker-sonnet`

**Profile rationale** — Standard persistence work against a known driver; every
criterion is a deterministic test.

**Implementation guidance**

- Add the driver with **`go get modernc.org/sqlite@v1.39.0` — pinned, exactly
  this version.** The unpinned latest requires go1.25 and will fail to build.
  After `go mod tidy`, confirm the directive is still `go 1.23.5`; if it changed,
  the pin was lost.
- `sql.Open("sqlite", path)` then `db.SetMaxOpenConns(1)` (SPEC §5). Schema
  created idempotently at open; no manual SQL prerequisite (SPEC §5).
- Store timestamps with the T1 fixed-width layout so `ORDER BY` on the text
  column is chronological.
- List ordering is `created_at DESC, id ASC`; filters are `status` and `type`,
  independently and combined. Filter *validation* (400 on a bad value) is the
  API's job — the store may assume valid input or reject with a typed error, but
  must not silently ignore an unrecognized filter.
- Health ping = a trivial query (`PingContext` or `SELECT 1`). Return a typed
  error; never surface raw SQLite text to callers (SPEC §30).
- Tests use `t.TempDir()`, one database file per test (SPEC §39). With
  `MaxOpenConns(1)`, two live handles on one file can block — do not share files
  across tests.
- A claim-supporting index on `(status, queued_at, id)` is fine; do not add more.

**Acceptance criteria**

- A fresh path produces a working database with no manual setup.
- A created job is retrievable by id; an unknown id yields a distinguishable
  not-found error.
- Payload, result, error, attempt count, all five timestamps, status, and type
  survive close-and-reopen byte-identically.
- Listing with no filter, `status` only, `type` only, and both returns exactly
  the matching jobs, ordered `created_at DESC, id ASC` — **with a test that
  constructs an actual `created_at` tie** so the id tie-break is exercised rather
  than assumed.
- The health check succeeds against an open database and fails against a closed
  one.

**Task-specific verification**

```bash
test -z "$(gofmt -l .)" && go build ./... && go vet ./... && go test -race ./internal/store/ -count=1
```

**Completion condition** — the command above passes, criteria satisfied in the
diff, `go.mod` pins `modernc.org/sqlite v1.39.0` and still reads `go 1.23.5`.

**Escalation** — `worker-sonnet` → `escalation-opus` → STOP.
**Downstream if stopped** — T3–T9.

---

## Task T3: Atomic state transitions and startup recovery

**Objective** — Every state transition in the system, implemented as a
concurrency-safe conditional update, plus startup recovery.

**Scope** — `internal/store` (transition and recovery methods) and its tests.

**Dependencies** — T2.
**Parallelizable** — No.
**Complexity** high · **Ambiguity** low · **Risk** **high**
**Initial execution profile** — `worker-sonnet`

**Profile rationale** — High risk, but low ambiguity: the required primitive is a
single guarded `UPDATE`, already verified in this environment (§1), and every
acceptance criterion is a deterministic test. Sonnet with an explicit primitive
is the right tier; `escalation-opus` is one bounded attempt away if the
concurrency tests fail.

**Implementation guidance**

- **The primitive, verified in this environment:** a guarded
  `UPDATE … WHERE id=? AND status=?` under `SetMaxOpenConns(1)` yields exactly one
  winner among concurrent callers, reported by `RowsAffected()`. Eight racing
  goroutines produced exactly one winner in a probe. SPEC §15 forbids
  read-then-write; there must be no `SELECT`-then-`UPDATE` on any transition path.
- Methods needed: claim (`QUEUED→RUNNING`, guarded on `attempt_count<3`,
  incrementing it and setting `started_at` **in the same statement** — SPEC §14
  requires the increment be atomic with the transition); complete
  (`RUNNING→COMPLETED`); fail (`RUNNING→FAILED`); cancel
  (`QUEUED|RUNNING→CANCELLED`); retry (`FAILED→QUEUED`, guarded on
  `attempt_count<3`).
- Each returns enough for the caller to distinguish *won* / *lost because the row
  moved on* / *not found*. The API maps these to 200/409/404 and cannot do so if
  they collapse into one error.
- Retry clears `started_at`, `finished_at`, `result`, `error`, sets a fresh
  `queued_at`, and leaves `attempt_count` untouched (SPEC §27).
- Cancel clears both `result` and `error` (SPEC §11).
- Recovery: one statement moving every `RUNNING` row to `FAILED` with
  `INTERRUPTED_EXECUTION`, `result=NULL`, fresh `finished_at`/`updated_at`,
  preserving `attempt_count`, `created_at`, `queued_at`, and `started_at`.
- Claim ordering is `queued_at ASC, id ASC`, and must never select a row at
  `attempt_count=3`.

**Acceptance criteria**

- N goroutines claiming one queued job: exactly one succeeds; `attempt_count`
  ends at exactly 1.
- N concurrent retries of one failed job: exactly one succeeds, the rest report a
  lost race; the job appears once in the queue.
- N concurrent cancels of one job: all observe a final `CANCELLED`; no invalid
  transition occurs.
- A completion attempt against a job already moved out of `RUNNING` does not
  overwrite it.
- Claiming honours `queued_at ASC, id ASC`, including a constructed `queued_at`
  tie.
- A job at `attempt_count=3` is never claimed and cannot be retried.
- Recovery converts `RUNNING` to `FAILED` with `INTERRUPTED_EXECUTION`, `result`
  null, `finished_at` set, `attempt_count` unchanged (SPEC §41).

**Task-specific verification**

```bash
test -z "$(gofmt -l .)" && go build ./... && go vet ./... && go test -race ./internal/store/ -count=1
```

Run the concurrency tests at `-count=5` before reporting.

**Completion condition** — the command above passes, the concurrency criteria
hold across repeated runs, and the diff shows no read-then-write transition path.

**Escalation** — `worker-sonnet` → `escalation-opus` → STOP.
**Downstream if stopped** — T4–T9.

---

## Task T4: Worker pool and running-job cancellation coordination

**Objective** — A bounded worker pool that claims and executes jobs, and the
in-process cancellation registry that makes SPEC §26's forbidden race window
unreachable.

**Scope** — `internal/worker/*.go` + tests.

**Dependencies** — T3.
**Parallelizable** — Yes, safely alongside T5 (disjoint packages); but see §4 for
why sequential is recommended anyway.
**Complexity** high · **Ambiguity** **high** · **Risk** **high**
**Initial execution profile** — **`escalation-opus` (direct)**

**Profile rationale — the one direct-Opus assignment.** SPEC §26 simultaneously
leaves the synchronization design open ("the exact synchronization design is
implementation-specific") and forbids a specific interleaving. That combination —
high ambiguity over a correctness property — is what the escalation tier exists
for. It is also the one place where the completion gate provides no safety net: a
missed registration window is probabilistic, and a passing suite would not detect
it, so an incorrect implementation ships silently. The registry contract is
consumed by both the API (T6) and shutdown (T7), making rework expensive. Sonnet
is not a bad fit for the *code*; it is a bad fit for being the last line of
defence on an unfalsifiable race.

**Implementation guidance**

- Exactly `WORKER_COUNT` goroutines, created once. No goroutine per job, no
  unbounded growth (SPEC §32).
- **The ordering rule that closes SPEC §26** — register the job's `CancelFunc` in
  the registry *before* attempting the claim `UPDATE`, and unregister if the claim
  is lost; the API signals cancellation *only after* winning the DB transition.
  Then: cancel-wins-while-queued makes the claim find 0 rows, and
  claim-wins-first guarantees the registry entry is already present when the
  signal arrives. Document this invariant next to the code (SPEC §45).
- Check `ctx.Err()` before beginning meaningful execution — the signal may
  already have landed.
- Attempt the terminal transition *before* unregistering.
- When execution returns a context-cancellation error, the worker may uniformly
  attempt `RUNNING→FAILED` with `SERVER_SHUTDOWN`: if a user cancel caused it, the
  row is already `CANCELLED` and the guarded update is a harmless no-op, so only
  genuine shutdown interruption records `SERVER_SHUTDOWN`. This avoids a fragile
  "why was I cancelled?" branch.
- A failing job must never take down a worker (SPEC §23). No automatic retry
  (SPEC §50).
- Poll with a short context-aware interval (tens of ms) so tests stay fast; stop
  claiming as soon as the pool context is cancelled.
- Log the SPEC §36 job events with `job_id`, `job_type`, and `attempt_count`;
  never log whole payloads.

**Acceptance criteria**

- Many workers, one queued job → exactly one execution and `attempt_count == 1`.
- With `WORKER_COUNT > 1`, N independent delay jobs finish in demonstrably less
  than N × duration (SPEC §40).
- A cancel issued against a running delay job returns the job promptly, well
  before its delay elapses, and the final state is `CANCELLED`.
- A cancel that wins while the job is still `QUEUED` results in the job never
  executing.
- A `fail` job leaves the pool alive and still processing subsequent jobs.
- Stopping the pool halts new claims while in-flight jobs finish or are
  interrupted; no goroutine leaks past stop.
- Repeated runs under `-race` report no findings.

**Task-specific verification**

```bash
test -z "$(gofmt -l .)" && go build ./... && go vet ./... && go test -race ./internal/worker/ -count=1
```

Run race-sensitive tests at `-count=10` before reporting.

**Completion condition** — the command above passes, the registration-ordering
invariant is present and documented in the code, and repeated `-race` runs are
clean.

**Escalation** — Already at the top tier. An unsuccessful bounded attempt is
**STOP / human** — write `.claude/escalations/<task-id>.md`.
**Downstream if stopped** — T6, T7, T8, T9 must not proceed. T5 may still run.

---

## Task T5: HTTP foundation — routing, error envelope, health, create, get

**Objective** — The HTTP layer's skeleton and the first three endpoints, with
correct routing and error semantics.

**Scope** — `internal/api/*.go` + tests. `POST /jobs`, `GET /jobs/{id}`,
`GET /health`, routing, error rendering.

**Dependencies** — T3.
**Parallelizable** — Yes, with T4.
**Complexity** medium · **Ambiguity** medium · **Risk** medium
**Initial execution profile** — `worker-sonnet`

**Profile rationale** — Standard stdlib HTTP work; the one non-obvious trap is
supplied below rather than left to discovery.

**Implementation guidance**

- **Verified trap:** with `net/http.ServeMux`, registering method-qualified
  patterns (`"POST /jobs"`) *plus* a `"/"` catch-all silently disables the
  built-in 405 — a probe showed `DELETE /jobs` returning 404 instead of the
  required 405. Register **path-only** patterns and switch on method inside each
  handler, emitting 405 with an explicit `Allow` header and the JSON envelope;
  reserve `"/"` for `ROUTE_NOT_FOUND`. Test `DELETE /jobs` explicitly.
- Every body-bearing response sets `Content-Type: application/json`, including
  errors (SPEC §16).
- API error codes (SPEC §30) are a separate namespace from T1's job error codes.
  Mapping: unknown route → 404 `ROUTE_NOT_FOUND`; unknown job → 404
  `JOB_NOT_FOUND`; bad method → 405 `METHOD_NOT_ALLOWED`; bad media type → 415
  `UNSUPPORTED_MEDIA_TYPE`; malformed / trailing data / unknown top-level field →
  400 `INVALID_JSON`; unknown `type` → 400 `INVALID_JOB_TYPE`; bad payload → 400
  `INVALID_PAYLOAD`; unexpected → 500 `INTERNAL_ERROR`.
- `Content-Type` must be `application/json`, accepting parameters like
  `; charset=utf-8` — parse with `mime.ParseMediaType`, do not string-compare.
- Body: exactly one JSON object; trailing non-whitespace rejected (decode, then
  confirm the decoder is at EOF); unknown fields rejected at both levels.
- Validate fully *before* insert (SPEC §17); respond 201 with the created
  snapshot showing `QUEUED` and `attempt_count=0`.
- `GET /jobs/{id}` with a malformed UUID is a 404, not a 400 — no UUID parsing
  needed.
- Health: 200 `{"status":"ok"}`, or 503 with the error envelope and
  `PERSISTENCE_UNAVAILABLE`. Test the failure branch by closing the database
  handle. Never leak SQLite text, SQL, or the database path.
- Tests use `httptest` against a real temp-file store; no manually started server
  (SPEC §38).

**Acceptance criteria**

- Valid hash, delay, and fail creates return 201 with the specified snapshot.
- Malformed JSON, unknown fields (top level and payload), unknown type,
  out-of-range delay, and a bad media type each return the specified status *and*
  error code.
- Get returns 200 for a known id, 404 for unknown and for malformed ids.
- Health returns 200 when persistence is available and 503 with
  `PERSISTENCE_UNAVAILABLE` when it is not.
- Unknown routes return 404 `ROUTE_NOT_FOUND`; known routes with unsupported
  methods return 405 with an `Allow` header — both as JSON.

**Task-specific verification**

```bash
test -z "$(gofmt -l .)" && go build ./... && go vet ./... && go test -race ./internal/api/ -count=1
```

**Completion condition** — the command above passes and a `DELETE /jobs` test
demonstrably returns 405 with `Allow`, not 404.

**Escalation** — `worker-sonnet` → `escalation-opus` → STOP.
**Downstream if stopped** — T6–T9.

---

## Task T6: List, cancel, and retry endpoints

**Objective** — The three remaining endpoints, including their race semantics.

**Scope** — `internal/api` (handlers + tests) only.

**Dependencies** — T4 and T5.
**Parallelizable** — No.
**Complexity** medium · **Ambiguity** low · **Risk** medium
**Initial execution profile** — `worker-sonnet`

**Profile rationale** — The hard primitives already exist in T3 and T4; this is
mapping them onto HTTP semantics.

**Implementation guidance**

- Reuse T5's envelope and routing; do not restructure them.
- Filters validate against the exact enumerations; anything else is 400
  `INVALID_FILTER`. **Assumption to follow:** unrecognized query-parameter
  *names* are ignored — SPEC §19 constrains values only. Response is always
  `{"jobs":[...]}`, with `[]` and never `null` when empty.
- Cancel: 200 with the `CANCELLED` job when the transition is won; 200
  idempotently when already `CANCELLED`; 409 `INVALID_STATE_TRANSITION` for
  `COMPLETED`/`FAILED`; 404 unknown. **Signal the registry only after the DB
  transition is won** (T4's contract). A 200 must guarantee `CANCELLED` is final.
- Retry: 200 for `FAILED` with `attempt_count<3`; 409 `ATTEMPT_LIMIT_REACHED` at
  3; 409 `INVALID_STATE_TRANSITION` otherwise; 404 unknown. Not idempotent —
  concurrent retries must produce exactly one 200 (SPEC §28).
- **Assumption to follow:** cancel and retry take no body and do not require
  `Content-Type` — SPEC §10 scopes that rule to `POST /jobs`.

**Acceptance criteria**

- List covers: no filter, status filter, type filter, combined filters, invalid
  status, invalid type, and deterministic ordering including a constructed tie.
- Cancel covers: queued, running, already-cancelled, completed, failed, unknown.
- Retry covers: failed below limit, failed at limit, non-failed, unknown.
- Concurrent cancels all agree on a final `CANCELLED`.
- Concurrent retries yield exactly one 200 and no duplicate queue entry.

**Task-specific verification**

```bash
test -z "$(gofmt -l .)" && go build ./... && go vet ./... && go test -race ./internal/api/ -count=1
```

**Completion condition** — the command above passes and every SPEC §38 Cancel,
Retry, and List case has a corresponding test.

**Escalation** — `worker-sonnet` → `escalation-opus` → STOP.
**Downstream if stopped** — T7, T8, T9.

---

## Task T7: Composition root — configuration, startup ordering, graceful shutdown

**Objective** — `main.go` wires everything with the specified startup ordering and
shutdown sequence, invocable deterministically from tests.

**Scope** — `main.go` (plus a small `app.go` if useful) and `package main` tests.

**Dependencies** — T6.
**Parallelizable** — No.
**Complexity** high · **Ambiguity** low · **Risk** high
**Initial execution profile** — `worker-sonnet`

**Profile rationale** — High blast radius but low ambiguity: SPEC §34 and §35
enumerate the required orderings step by step, so this is careful assembly
against an explicit checklist rather than open design.

**Implementation guidance**

- Config: `PORT` (default 8080, valid 1–65535), `WORKER_COUNT` (default 4, valid
  1–64), `DATABASE_PATH` (default `./taskforge.db`; **set-but-empty is invalid**,
  unset is not). Invalid values exit non-zero with a clear message before
  anything else starts.
- Startup order is load-bearing: open and initialize the DB → **run recovery** →
  *then* start the HTTP listener → *then* start the workers (SPEC §34). Neither
  the listener nor a worker may observe a `RUNNING` row left by a previous
  process.
- Shutdown, in SPEC §35 order: stop claiming → `http.Server.Shutdown` → signal
  running jobs → let workers record `SERVER_SHUTDOWN` where still current → wait
  for workers → close the DB → exit. The whole sequence is bounded at 10s. Queued
  jobs stay `QUEUED`. A user cancellation, completion, or failure that already won
  must not be overwritten — the guarded updates give you this for free.
- SPEC §42 requires deterministic invocation without OS signals: structure the app
  so a test can start it and call shutdown directly. `signal.NotifyContext` for
  `SIGINT`/`SIGTERM` belongs in `main` only, as a thin wrapper.
- Shutdown tests must have a hard upper bound so a hang fails the test rather
  than consuming the gate's 300s budget.
- Log the SPEC §36 lifecycle events via `log/slog`.

**Acceptance criteria**

- Each invalid config value exits non-zero with a clear message; each valid one
  starts.
- Recovery provably completes before the listener accepts and before any claim.
- `go run .` starts with defaults on a clean checkout.
- Shutdown stops further claiming, interrupts a running delay job, records
  `SERVER_SHUTDOWN` when it wins the transition, leaves queued jobs `QUEUED`,
  does not overwrite an already-won terminal state, and returns within the
  timeout.
- A job persisted as `RUNNING` becomes `FAILED` with `INTERRUPTED_EXECUTION` on
  the next start.

**Task-specific verification**

```bash
test -z "$(gofmt -l .)" && go build ./... && go vet ./... && go test -race ./... -count=1
```

**Completion condition** — the command above passes and the shutdown sequence is
invocable from a test without OS signals.

**Escalation** — `worker-sonnet` → `escalation-opus` → STOP.
**Downstream if stopped** — T8, T9.

---

## Task T8: Cross-cutting concurrency and race tests

**Objective** — Close out SPEC §40's end-to-end race requirements not already
covered by T3, T4, and T6.

**Scope** — Test files only. No production code changes; if a test exposes a real
defect, report it rather than widening scope.

**Dependencies** — T7.
**Parallelizable** — Yes, with T9.
**Complexity** medium · **Ambiguity** medium · **Risk** medium
**Initial execution profile** — `worker-sonnet`

**Profile rationale** — Test authorship against primitives that already exist;
the design judgement (assert the invariant, not the winner) is supplied below.

**Implementation guidance**

- Remaining items: cancellation-vs-completion, cancellation-vs-failure, and
  end-to-end attempt-count safety under concurrent worker activity.
- **Assert the invariant, not the winner.** Drive a short job, fire cancel at a
  small offset, iterate ~50 times, and assert: the final state is in the
  permitted set; a 200 from cancel implies a permanently `CANCELLED` job; a 409
  implies the pre-existing `COMPLETED`/`FAILED` state is unchanged;
  `attempt_count` never exceeds one increment per execution. Pinning a specific
  winner would be flaky by construction.
- Keep every delay near the 100ms floor. The gate allows 300s for the whole
  suite and iteration counts multiply quickly.
- A failing assertion here means a real defect in T3, T4, or T6 — do not relax
  the test (SPEC §43, §44).

**Acceptance criteria**

- Both race scenarios are exercised repeatedly, with the API response always
  agreeing with the persisted outcome.
- Attempt counts never double-increment under concurrent worker activity.
- `go test -race ./...` is clean across repeated runs.
- Total suite runtime stays comfortably under the gate's 300s.

**Task-specific verification**

```bash
test -z "$(gofmt -l .)" && go build ./... && go vet ./... && go test -race ./... -count=1
time go test ./...
```

**Completion condition** — the above passes, and the measured `go test ./...`
runtime is recorded in `EXECUTION_NOTES.md` against the 300s gate budget.

**Escalation** — `worker-sonnet` → `escalation-opus` → STOP.
**Downstream if stopped** — none (T9 may still run).

---

## Task T9: README

**Objective** — `README.md` covering all 20 items in SPEC §48, with an
architecture diagram.

**Scope** — `README.md` only.

**Dependencies** — T7.
**Parallelizable** — Yes, with T8.
**Complexity** low · **Ambiguity** low · **Risk** low
**Initial execution profile** — `worker-sonnet`

**Profile rationale** — Mechanical in shape but content-heavy, and required to be
*accurate* about concurrency, recovery, and shutdown semantics. A Haiku turn
budget spent reading four packages would likely produce a plausible-but-wrong
description.

**Implementation guidance**

- Root `README.md`. SPEC §48 and `CLAUDE.md` agree there is no collision with the
  harness documentation under `.claude/`.
- Document the design decisions SPEC §2 asks to be recorded: the
  `modernc.org/sqlite` **v1.39.0 pin and why** (newer releases require go1.25,
  which this environment must not install), `SetMaxOpenConns(1)`, the fixed-width
  timestamp layout and its ordering role, the guarded-update transition strategy,
  the registration-before-claim cancellation invariant, and hand-rolled UUIDv4
  instead of a dependency.
- Dependency list: `modernc.org/sqlite` is the only direct dependency.
- Known limitations: the single connection limits write throughput; the claim
  loop polls; no pagination; no automatic retry.

**Acceptance criteria**

- All 20 SPEC §48 items are present.
- A Mermaid or ASCII architecture diagram is included.
- The documented commands are the ones that actually work in this repository.
- Example requests match the implemented contract.

**Task-specific verification**

```bash
test -z "$(gofmt -l .)" && go build ./... && go vet ./... && go test ./... -count=1
```

Plus a read-through against the SPEC §48 list.

**Completion condition** — the command above passes and all 20 items are present
and accurate.

**Escalation** — `worker-sonnet` → `escalation-opus` → STOP.
**Downstream if stopped** — none.

---

# 4. Execution graph and waves

```
T1 domain/module
 └── T2 store foundation
      └── T3 atomic transitions + recovery
           ├── T4 worker pool + cancellation  [OPUS]
           └── T5 api foundation
                └── T6 list/cancel/retry   (also needs T4)
                     └── T7 composition root
                          ├── T8 concurrency tests
                          └── T9 README
```

| Wave | Tasks |
|---|---|
| 1 | T1 |
| 2 | T2 |
| 3 | T3 |
| 4 | T4, T5 |
| 5 | T6 |
| 6 | T7 |
| 7 | T8, T9 |

**Recommendation: run every wave sequentially, including wave 4.** The completion
gate is repository-global (`go test ./...`) and cannot attribute a failure to a
task, so two concurrent workers make each other's gate result uninterpretable —
and a worker that leaves the tree non-compiling blocks *both* closures.
T4∥T5 and T8∥T9 are the only genuinely disjoint pairs. If wall-clock ever
matters, T8∥T9 is the safer of the two (test files vs. a Markdown file), and T9
should be closed first.

---

# 5. Risk and ambiguity summary

**Highest-risk tasks**

- **T4** — the SPEC §26 race window is not detectable by the gate. Mitigated by
  the registration-ordering invariant (§2), repeated `-race` runs, and the Opus
  tier.
- **T3** — every downstream guarantee rests on it. Fully covered by deterministic
  concurrency tests, and the primitive is already verified in this environment.
- **T7** — a hang here consumes the gate's 300s budget for every later task.
  Mitigated by hard per-test time bounds.

**Material assumptions** (each safe to proceed under; all to be documented in the
README)

1. `modernc.org/sqlite` **v1.39.0**, pinned — newer releases require go1.25 and
   SPEC §3 forbids switching toolchains.
2. `go test -race` needs cgo for the race runtime; SPEC §3's CGO prohibition
   governs the application and the driver, both of which stay pure Go.
3. Top-level unknown fields → `INVALID_JSON`; payload unknown fields →
   `INVALID_PAYLOAD`. SPEC §30 does not disambiguate.
4. `payload` is required on `POST /jobs`, including for `fail` (`{}`), per SPEC
   §10's "missing required fields must be rejected".
5. Unrecognized query-parameter *names* on `GET /jobs` are ignored; SPEC §19
   constrains values only.
6. Cancel and retry require no `Content-Type` and ignore any body; SPEC §10
   scopes that rule to `POST /jobs`.
7. UUIDv4 from `crypto/rand` rather than a new direct dependency (SPEC §47).
8. Timestamps are fixed-width nanosecond RFC 3339 in UTC, satisfying "sufficient
   precision to preserve ordering".

**Unresolved ambiguities** — none that prevent implementation. All eight above
are documentable assumptions.

**Cross-task risks**

- `internal/store` is touched by T2 and T3, and `internal/api` by T5 and T6 — in
  both cases sequentially, so no conflict.
- The global gate couples all tasks; this is why sequential execution is
  recommended.
- T4's registry contract is consumed by T6 and T7, so a T4 stop halts all three.
- The `go 1.23.5` directive can be silently bumped by any careless `go get`.
  Check it after every task (§0.3 step 5).

---

# 6. Resource allocation

| Task | Profile | Complexity | Ambiguity | Risk | Depends on |
|---|---|---|---|---|---|
| T1 domain + module | worker-sonnet | medium | low | medium | None |
| T2 store foundation | worker-sonnet | medium | low | medium | T1 |
| T3 atomic transitions | worker-sonnet | high | low | high | T2 |
| **T4 worker + cancellation** | **escalation-opus** | high | **high** | **high** | T3 |
| T5 api foundation | worker-sonnet | medium | medium | medium | T3 |
| T6 list/cancel/retry | worker-sonnet | medium | low | medium | T4, T5 |
| T7 composition root | worker-sonnet | high | low | high | T6 |
| T8 concurrency tests | worker-sonnet | medium | medium | medium | T7 |
| T9 README | worker-sonnet | low | low | low | T7 |

**T4 is the only direct-Opus assignment**, justified in its Profile rationale: the
specification explicitly delegates the synchronization design while forbidding a
specific interleaving, and the completion gate cannot falsify a missed race
window.

**No task is assigned to `worker-haiku`.** After folding the module bootstrap into
T1 — forced by the finding that `go test ./...` exits 1 with no packages — no
remaining unit is mechanical enough to justify it, and manufacturing one would
add a vacuous gate pass for no benefit.

---

# 7. Verification strategy

**Task level** — one batched command per task:

```bash
test -z "$(gofmt -l .)" && go build ./... && go vet ./... && go test -race ./<pkg>/ -count=1
```

Batched deliberately. Per `.claude/TASK_PLANNING_INSTRUCTIONS.md`'s own record, a
worker given a checklist of separate checks executes the checklist instead of the
deliverable.

**Repository completion gate** — `TaskCompleted` runs `go test ./...` with a 300s
limit. Note what it does *not* cover: `-race`, `go vet`, `gofmt`, and
`go build`. Those must come from the task-level command, run by the orchestrator.

**Specification-level verification** — after T9, run by the orchestrator:

```bash
go build ./...
go test ./...
go test -race ./...
go vet ./...
gofmt -l .                      # must print nothing
git diff --stat main..HEAD -- .claude/   # only test-command.conf
grep '^go ' go.mod              # go 1.23.5
```

Then: start `go run .` on a clean checkout with defaults and exercise all six
endpoints; verify each config-failure case exits non-zero; and walk the 27-line
SPEC §49 Definition of Done, recording evidence for each line in
`EXECUTION_NOTES.md`.

**Final report** (SPEC §51) — architecture summary, major implementation
decisions, verification commands executed, test results, race-detector results,
static-analysis results, known limitations, and any requirement not fully
satisfied.
