# TaskForge — Session Statistics

Run record for the `experiment/go-job-processing-service` arm.
Session `8738e7a0-2406-4009-88ab-8b76ac2efba6`, Claude Code `2.1.233`,
apparatus `88abd63`.

**Provenance of every number below is labelled.**

- **Measured** — read from a file, a command, or the harness's own event log.
- **Derived** — arithmetic over measured values.
- **Estimated** — engineering judgment, with assumptions stated.

Nothing here is taken from a subagent's self-report; worker claims were
independently re-run by the orchestrator throughout (see `EXECUTION_NOTES.md`).

---

## 1. Deliverable

*Measured.*

| | Files | Lines |
|---|---|---|
| Production Go | 23 | **2,473** |
| Test Go | 16 | **4,832** |
| **Total Go** | **39** | **7,305** |

Test-to-production ratio: **1.95 : 1**.

By package:

| Package | Production | Test | Tests |
|---|---:|---:|---:|
| `.` (composition root) | 350 | 582 | 12 |
| `internal/jobs` | 517 | 692 | 25 |
| `internal/store` | 596 | 1,062 | 28 |
| `internal/worker` | 534 | 732 | 20 |
| `internal/api` | 476 | 1,764 | 50 |
| **Total** | **2,473** | **4,832** | **135** |

**135 top-level tests, 207 including table-driven subtests.**

Documentation authored this session:

| File | Lines | Purpose |
|---|---:|---|
| `PLAN.md` | 1,017 | task decomposition + execution contract |
| `EXECUTION_NOTES.md` | 934 | run record required by the harness |
| `README.md` | 389 | SPEC §48 deliverable (20 items + diagram) |
| `.claude/escalations/1.md` | 172 | human escalation record |
| **Total** | **2,512** | |

**Grand total authored: 9,817 lines** across 43 files.

Build output: single static binary, 14 MB, no CGO, one direct dependency
(`modernc.org/sqlite v1.39.0`).

---

## 2. Timing

*Measured* from `.claude/metrics/task-events.jsonl` (the harness's own gate log),
file mtimes, and subagent transcript timestamps.

### Session envelope

| | Timestamp (UTC) |
|---|---|
| Session start (SPEC handed over) | 2026-08-15 14:32:24 |
| Session end (final record written) | 2026-08-16 02:08:25 |
| **Total wall clock** | **11 h 36 m** |

That figure is misleading on its own. The session contained two long pauses while
the user waited for a usage-budget refresh:

| Phase | Window | Duration |
|---|---|---|
| Planning (spec analysis, repo inspection, 6 environment probes, `PLAN.md`) | 14:32 → ~14:50 | ~18 m |
| *Idle — user awaiting budget refresh* | ~14:50 → 19:28 | ~4 h 38 m |
| Execution A (T1–T6) | 19:28 → 20:57 | ~1 h 29 m |
| *Idle — user awaiting budget refresh* | 20:57 → ~01:20 | ~4 h 23 m |
| Execution B (T7–T9 + final verification) | ~01:20 → 02:08 | ~48 m |

**Active session time: ~2 h 35 m.** Idle: ~9 h 01 m. *(Derived.)*

Active time includes orchestrator work (dispatching, independent verification,
mutation testing, record-keeping) and short user-response gaps inside the
windows; it excludes the two long pauses.

### Gate-passage timeline

*Measured — verbatim from `task-events.jsonl`. Every entry is `gate_passed`;
there are no failed completion attempts recorded, because refused tasks never
reached closure.*

| Task | Gate passed (UTC) | Δ from previous |
|---|---|---|
| T1 domain + module | 2026-08-15 19:47:58 | — |
| T2 store foundation | 2026-08-15 20:06:06 | 18 m 08 s |
| T3 atomic transitions | 2026-08-15 20:13:14 | 7 m 08 s |
| T4 worker + cancellation | 2026-08-15 20:32:47 | 19 m 33 s |
| T5 api foundation | 2026-08-15 20:43:01 | 10 m 14 s |
| T6 list/cancel/retry | 2026-08-15 20:56:57 | 13 m 56 s |
| T7 composition root | 2026-08-16 01:53:53 | *(4 h 57 m — includes the idle pause)* |
| T9 README | 2026-08-16 02:06:42 | 12 m 49 s |
| T8 race tests | 2026-08-16 02:06:49 | 7 s *(ran in parallel with T9)* |

**Nine tasks closed; nine gate passes; zero gate failures at closure.**
Median time between consecutive closures in a single active window: **13 m 56 s**.

---

## 3. Agent execution

*Measured — from subagent completion records.*

| # | Task | Model | Tool uses | Wall clock | Tokens |
|---|---|---|---:|---:|---:|
| 1 | T1 attempt 1 | sonnet | 18 | 2 m 08 s | 37,649 |
| 2 | T1 attempt 2 (escalation) | **opus** | 19 | 4 m 25 s | 45,465 |
| 3 | T1 attempt 3 | sonnet | 15 | 0 m 31 s | 40,774 |
| 4 | T1 attempt 4 (narrow repair) | sonnet | 5 | 0 m 14 s | 21,219 |
| 5 | Turn-budget calibration probe | sonnet | 30 | 0 m 52 s | 25,266 |
| 6 | T2 | sonnet | 18 | 2 m 55 s | 43,265 |
| 7 | T2 send-back | sonnet | 4 | 0 m 26 s | 47,756 ¹ |
| 8 | T3 | sonnet | 35 | 4 m 51 s | 90,728 |
| 9 | T4 | **opus** | 53 | 16 m 35 s | 116,062 |
| 10 | T5 | sonnet | 34 | 3 m 41 s | 65,089 |
| 11 | T6 attempt 1 | sonnet | 40 | 3 m 22 s | 80,561 |
| 12 | T6 attempt 2 (escalation) | **opus** | 40 | 7 m 36 s | 103,072 |
| 13 | T7 | sonnet | 40 | 27 m 19 s | 89,639 |
| 14 | T8 | sonnet | 43 | 9 m 20 s | 108,038 |
| 15 | T9 | sonnet | 19 | 1 m 31 s | 58,259 |
| | **Total** | | **413** | **1 h 25 m 47 s** | **972,842** |

¹ The send-back resumed an existing agent. Whether 47,756 is that invocation's
own usage or the agent's cumulative total is ambiguous from the record; if
cumulative, the true grand total is ~4,500 tokens lower. Flagged rather than
silently resolved.

**By model tier** *(derived)*:

| Tier | Invocations | Tokens | Share |
|---|---:|---:|---:|
| Sonnet | 12 | 708,243 | 72.8 % |
| Opus | 3 | 264,599 | 27.2 % |

14 distinct subagents, 15 invocations. **Subagent wall clock (1 h 26 m) is 55 %
of active session time (2 h 35 m)** — the remainder is orchestration and
independent verification.

### What I cannot measure

Stated plainly, because it bears directly on cross-checking your telemetry:

- **Orchestrator token usage is not visible to me.** The 972,842 figure covers
  subagents only. My own planning, verification, mutation testing and
  record-keeping are excluded, and that is a substantial share of the run.
- **No dollar figure.** This session authenticates by subscription
  (`CLAUDE_CODE_OAUTH_TOKEN`), so there is no per-token invoice to read. For an
  API-equivalent costing, apply your rates to the per-tier token split above —
  but note it will understate the total by the orchestrator's usage.
- **Cache behaviour is invisible to me**, so token counts do not distinguish
  cache reads from fresh input.

Your OTEL pipeline should have all three. **These numbers are independent of it
— that is what makes them useful for validating it.** If Grafana disagrees with
the table above on subagent count (15 invocations), tool uses (413), or the
gate-passage timeline (9 events), the discrepancy is in the collector, not here.

---

## 4. Process metrics

*Measured.*

| Metric | Value |
|---|---|
| Tasks planned | 9 |
| Tasks completed | 9 |
| Tasks abandoned | 0 |
| Total dispatches | 14 (excluding the calibration probe) |
| **Dispatches per task (mean)** | **1.56** |
| Tasks completed in one dispatch | 6 of 9 (T3, T4, T5, T7, T8, T9) |
| Escalations to Opus | 3 (T1, T4-direct, T6) |
| Human escalations | 1 (T1, resolved by raising turn budgets) |
| Send-backs (gate passed, criterion unmet) | 1 (T2) |
| Turn-budget exhaustions | 5 (T1 ×3, T6 ×1, T7 ×1) |
| Apparatus violations by any task | **0** |
| Specification deviations | 1 (`EXECUTION_ERROR`, documented) |
| Tests weakened, skipped or deleted | **0** |

### Verification performed by the orchestrator, not the workers

| Check | Count |
|---|---|
| Independent full-suite runs | 12+ |
| Mutation tests run to falsify a claimed invariant | 2 (`confirmRunning` guard; `Allow` header) |
| Live end-to-end runs of the real binary | 1 (16 endpoint/error cases) |
| Substance gaps found that the gate did not catch | 2 (T2 reopen coverage; T6 untested handlers) |

### Suite performance

*Measured, cold cache.*

| | Time |
|---|---|
| `go test ./...` | 5.9 s |
| `go test -race ./...` | 9.5 s |
| Gate budget (`TEST_TIMEOUT_SECONDS`) | 300 s |
| **Headroom** | **~31× / ~50×** |

---

## 5. Estimated equivalent developer effort

*Estimated.* This is engineering judgment, not measurement. The assumptions are
stated so you can adjust them.

### Assumptions

1. **One developer, working solo**, mid-to-senior, fluent in Go, comfortable with
   `database/sql`, `net/http`, `context` and goroutines. Not previously familiar
   with `modernc.org/sqlite`.
2. **Working from this same `SPEC.md`** — an unusually complete specification.
   Requirements discovery, stakeholder cycles and design debate are **excluded**.
   On a normal project that is often the largest cost.
3. **The same quality bar**: race-clean under `-race`, 135 tests including forced
   concurrency races, atomic-transition guarantees verified rather than asserted,
   and a 20-section README. This bar is well above typical.
4. **Focused engineering hours** — not calendar time, and excluding meetings,
   code review, CI setup, PR cycles and deployment.
5. No pairing, no prior implementation of this exact design to copy.

### Breakdown

| Area | Low | High | Notes |
|---|---:|---:|---|
| Module setup, dependency selection | 1 h | 2 h | the go1.25 toolchain trap alone can cost an hour |
| Domain: types, validation, executors, transitions | 4 h | 6 h | |
| Store: schema, CRUD, list, ping, persistence tests | 4 h | 6 h | |
| Atomic transitions + concurrency tests | 6 h | 10 h | where the real design thinking goes |
| Worker pool + cancellation coordination | 8 h | 14 h | hardest part; SPEC §26 is genuinely subtle |
| HTTP layer, validation, error envelope, tests | 6 h | 8 h | the ServeMux 405 masking trap is a half-day if unknown |
| Composition root, config, recovery, shutdown | 4 h | 6 h | |
| Cross-cutting race tests | 4 h | 6 h | |
| README (20 sections + diagram) | 2 h | 4 h | |
| Integration, debugging, polish | 3 h | 5 h | |
| **Total focused hours** | **42 h** | **67 h** | |

**≈ 42–67 focused engineering hours** — roughly **5–8.5 working days**, or
**1.5–2.5 calendar weeks** at a realistic 60–70 % focus ratio.

### What would move the estimate

**Lower.** A developer who has built this exact shape before could cut 30–40 %.
Scoping tests to typical coverage rather than 1.95 : 1 would remove perhaps
8–12 h — most teams would not write forced-race tests that log their win/loss
splits, nor mutation-test their own invariants.

**Higher, potentially much higher.** The SPEC §26 race window is the kind of
defect that does not fail deterministically. This run demonstrated the point
directly: when the guard was disabled, the timing-based stress test **still
passed** at `-count=5`, and every state assertion stayed green. A developer who
ships that bug loses it to intermittent CI failures, and finding it can cost
days rather than hours. The 8–14 h line item assumes they get it right, or catch
it fast.

### Comparison

| | Value |
|---|---|
| Active session time | ~2 h 35 m |
| Subagent execution within that | ~1 h 26 m |
| Estimated solo-developer equivalent | 42–67 h |
| **Implied ratio** | **~16× to ~26×** |

**Read that ratio carefully.** It is *not* "the agent is 20× a developer". Honest
qualifiers:

- The specification was exceptionally detailed, and writing it was real work not
  counted on either side.
- A human stayed in the loop throughout: approving the plan, resolving one
  escalation, and making the turn-budget call. Their time is inside the 2 h 35 m
  but their judgment is not substitutable.
- The run needed **one human escalation** and a mid-run configuration change to
  get past T1. Without those it would have stalled.
- Wall clock was 11 h 36 m; only 2 h 35 m of it was active. Budget limits, not
  capability, set the calendar.
- The 42–67 h estimate carries genuine uncertainty, particularly the concurrency
  line items.

The defensible claim is narrower and still strong: **a specified, race-tested,
7,300-line Go service with 135 passing tests and no unresolved requirements was
produced in about two and a half hours of active time, with one human decision
point.**

---

## 6. Quality evidence

*Measured — all commands run by the orchestrator on a cold cache after T9.*

```
go build ./...        PASS
go vet ./...          PASS
gofmt -l .            PASS (no output)
go test ./...         ok — 5 packages
go test -race ./...   ok — 5 packages
```

| Property | Status |
|---|---|
| SPEC §44 required checks | 5 of 5 pass |
| SPEC §49 Definition of Done | 27 of 27 items evidenced |
| Race-detector findings | 0 |
| `t.Skip` in the suite | 1, the idiomatic `testing.Short()` guard; verified it **runs** under the gate's command |
| Gate script identical to `main` | yes, byte-for-byte |
| `git diff main..HEAD -- .claude/` | only `test-command.conf`, as the design requires |
| Go directive unchanged | `go 1.23.5` throughout |
| Direct dependencies | 1 (`modernc.org/sqlite v1.39.0`) |

### Caveat for cross-arm comparison

The worker turn budgets were raised **mid-run** (haiku 8→15, sonnet 15→40, opus
15→50) as the resolution to T1's human escalation. This arm is therefore **not
directly comparable** to the warmup arms unless those are re-run at the new
budgets. `check-arm-ready.sh` guards `hooks/` but not `agents/`, so it will not
flag the divergence — its silence is not evidence of a shared apparatus.

Note also that T1 ran entirely under the old 15-turn budget and T2–T9 under the
new one, so T1's four dispatches should not be pooled with the rest when
computing per-task dispatch rates. **Excluding T1: 10 dispatches for 8 tasks,
1.25 per task.**
