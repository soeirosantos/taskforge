# TaskForge — Telemetry Statistics

OTEL/Prometheus measurements for the `experiment/go-job-processing-service` arm,
extracted before the metrics stack was stopped.

**Source:** Claude Code OTLP → OpenTelemetry Collector → Prometheus, over the
24 h window containing the run. Session
`8738e7a0-2406-4009-88ab-8b76ac2efba6`, Claude Code `2.1.233`, apparatus
`88abd63`, `linux/arm64`.

This file is the **pipeline's** view of the run. `STATS.md` is the
**orchestrator's** independently-measured view. They disagree, and the
disagreements are informative — see §5.

> **Read §6 before quoting any number from this file.** Several are lower bounds
> and one is wrong.

---

## 1. Headline

| Metric | Value | Confidence |
|---|---|---|
| **Total cost** | **$4.21** | lower bound (see §6.1) |
| **Total tokens** | **3,086,297** | lower bound |
| Lines added / removed | 1,640 / 33 | partial (see §6.3) |
| Edit-tool decisions | 12 accepted, 0 rejected | partial |
| Claude Code sessions | 1 | correct but easily misread (§6.2) |
| **Subscription consumption** | **~1.5 of the 5 h session windows** | operator-reported (§1.1) |

This is the only place a **dollar figure for the whole run** exists. Neither the
orchestrator nor `STATS.md` could produce one, because the session authenticates
by subscription and no per-token invoice is visible to it.

### 1.1 Subscription consumption — the binding constraint

*Operator-reported, not measured by any instrument in this repository.*

The run consumed roughly **one and a half Claude Pro 5-hour session windows**.
The operator used the subscription for essentially nothing else during this work,
so the figure is close to attributable in full.

**This, not the $4.21, is what actually limited the run.** The evidence is in the
timeline: wall clock was 11 h 36 m against ~2 h 35 m of active time, and the two
gaps — 4 h 38 m and 4 h 23 m — were spent waiting for the subscription budget to
refresh, not working. Capability did not set the calendar; the rate limit did.

Two things follow that the dollar figure alone would hide:

- **$4.21 of API-equivalent spend is not the price of this run to a Pro
  subscriber.** It is 1.5 five-hour windows, which for a solo user is most of a
  working day's allowance regardless of how few dollars the tokens represent.
- **The orchestrator's 70 % cost share (§2) is also a 70 % share of that
  allowance.** Anything that shortens orchestrator context — fewer verification
  re-runs, more aggressive summarization, splitting a long arm across sessions —
  buys back rate-limit headroom, which is the scarcer resource here.

For a run of this size the practical planning number is therefore **~1.5 session
windows per 7,300-line specified service**, with the caveat that this was a
single observation on an unusually complete specification.

*Not instrumented.* Claude Code does not export subscription-window consumption
as a metric, and it is not in `task-events.jsonl`. If it matters to future arms,
it has to be recorded by hand at the end of a run.

---

## 2. Cost by query source

The most useful split in the dataset, and the one that reconciles this file with
`STATS.md`.

| Source | Cost | Share | What it is |
|---|---:|---:|---|
| `main` | $2.97 | **70.4 %** | the orchestrator |
| `subagent` | $1.01 | 23.9 % | all 15 worker dispatches |
| `auxiliary` | $0.24 | 5.7 % | summarization and side queries |
| **Total** | **$4.21** | | |

**The orchestrator cost roughly 3× all of its workers combined.** That is the
single most consequential finding in this file, and it is invisible in
`STATS.md`, which measures subagents only.

It is a direct consequence of the design: `CLAUDE.md` requires the orchestrator
to run verification itself after every task rather than trust a worker's report.
That verification — plus planning, dispatch composition, substance review,
mutation testing and record-keeping — is where the money went.

### Cost by source × model

| Source | Model | Cost |
|---|---|---:|
| `main` | opus-5 | $2.9659 |
| `subagent` | sonnet-5 | $0.5074 |
| `subagent` | opus-5 | $0.4995 |
| `auxiliary` | opus-5 | $0.2042 |
| `auxiliary` | sonnet-5 | $0.0336 |
| `auxiliary` | haiku-4.5 | $0.0006 |

Note the subagent split: 12 Sonnet invocations cost about the same as 3 Opus
invocations ($0.507 vs $0.500). Opus is ~4× the per-dispatch cost here.

### Cost by model (all sources)

| Model | Cost | Share |
|---|---:|---:|
| opus-5 | $3.6696 | 87.1 % |
| sonnet-5 | $0.5410 | 12.8 % |
| haiku-4.5 | $0.0006 | 0.01 % |

**87 % Opus is not evidence of escalation overuse.** The orchestrator itself runs
on Opus and accounts for 70 % of total spend. Escalation-driven Opus is the
`subagent` row only: $0.50, under 12 % of the run.

---

## 3. Tokens

| Source | Tokens | Share |
|---|---:|---:|
| `main` | 1,331,008 | 43.1 % |
| `subagent` | 1,245,238 | 40.3 % |
| `auxiliary` | 510,051 | 16.5 % |
| **Total** | **3,086,297** | |

### By type

| Type | Tokens | Share |
|---|---:|---:|
| `cacheRead` | 2,613,669 | **84.7 %** |
| `cacheCreation` | 418,525 | 13.6 % |
| `output` | 45,922 | 1.5 % |
| `input` | 8,181 | 0.3 % |

Prompt caching carried the run: 85 % of all tokens were cache reads. Fresh input
was 0.3 %. This is why $4.21 buys 3 M tokens.

### By source × type

| Source | cacheRead | cacheCreation | output | input |
|---|---:|---:|---:|---:|
| `main` | 1,032,836 | 278,555 | 19,611 | 6 |
| `subagent` | 1,088,068 | 131,531 | 25,561 | 78 |
| `auxiliary` | 492,765 | 8,439 | 750 | 8,097 |

The orchestrator's `cacheCreation` is 2× the subagents' — it holds a long-lived
context that is repeatedly re-cached as the run progresses. That is the cost
shape of a single long orchestrating session.

### By model

| Model | Tokens |
|---|---:|
| opus-5 | 2,143,800 |
| sonnet-5 | 941,948 |
| haiku-4.5 | 549 |

---

## 4. Cross-arm comparison

All four arms that reported telemetry, over a 7 d window.

| Arm | Cost | Tokens |
|---|---:|---:|
| `warmup-go-healthcheck-2` | $2.29 | 2,729,988 |
| `warmup-go-healthcheck-3` | $0.60 | 1,298,155 |
| `warmup-rust-healthcheck` | $0.87 | 1,219,206 |
| **`go-job-processing-service`** | **$4.21** | **3,086,297** |

**These are not comparable as an experiment.** Three independent reasons:

1. **Different apparatus.** Turn budgets were raised mid-run for the job-service
   arm (haiku 8→15, sonnet 15→40, opus 15→50). The warmups ran at 15/8. See
   issue #8.
2. **`warmup-go-healthcheck-2` is a failed run**, not a cheap one. Its $2.29
   bought a container that vanished mid-run; most of that spend was apparatus
   debugging, not implementation.
3. **Wildly different scope.** The warmups are a health-check endpoint
   (~200 lines). The job service is 7,305 lines of Go with 135 tests.

The only defensible reading: a 7,300-line specified service with a full test
suite cost **$4.21** and about 2 h 35 m of active time.

Ignore the arms labelled `probe`, `fixprobe`, `smoke`, `dashcheck`, `verify` and
`main` — those are apparatus debugging from this session's development, not
experiment data.

---

## 5. Reconciliation with `STATS.md`

The two sources disagree, and every disagreement has an identified cause. None
is a pipeline defect.

| Quantity | Prometheus | `STATS.md` | Why |
|---|---:|---:|---|
| Tokens | 3,086,297 | 972,842 | `STATS.md` covers **subagents only** and says so; it cannot see orchestrator usage. Prometheus `subagent` alone is 1,245,238 — still higher, because it counts the calibration probe and per-invocation overhead the transcripts do not expose. |
| Sessions | 1 | 15 invocations | Subagents report under the **parent** session id. Prometheus counts Claude Code sessions; `STATS.md` counts dispatches. Both correct, different units. |
| Opus share | 87 % of cost | 27 % of subagent tokens | Different denominators. `STATS.md` measures subagents; the orchestrator is Opus and is 70 % of total spend. |
| Active time | 247 s | ~2 h 35 m | **Prometheus is wrong here.** See §6.4. |

Cross-validation that did hold: apparatus labels
(`sha=88abd63`, `cli=2.1.233`, `arch=arm64`) match the run exactly, and the arm
label is correct on every series — confirming the attribution fix in `88abd63`
works.

---

## 6. Accuracy notes and known defects

### 6.1 Cost and token totals are lower bounds

Claude Code exports these as counters, but the exported series **reset between
collector windows** rather than accumulating monotonically across the whole
session. Inspecting the raw range query shows individual series where a later
sample is *lower* than an earlier one (e.g. `subagent`/opus: 0.4072 → 0.2478).

`max_over_time(...[24h])` therefore captures the largest value each series
reached, not the true sum over the run. Where a series reset more than once, the
earlier segment is lost.

**Consequence: $4.21 and 3.09 M tokens are floors. The true totals are higher,
by an unknown amount.** Every per-source and per-model breakdown inherits the
same caveat, though the *proportions* are likely to be roughly right since all
series are affected similarly.

This also makes the cost-over-time panel misleading: it shows five disconnected
segments rather than a cumulative curve.

### 6.2 "1 session" is correct but easily misread

Subagents do not open their own Claude Code session, so a nine-task run with 15
dispatches reports as one session. The dashboard's *Sessions* stat panel will
always read 1 for a run like this. It is not a count of agents or dispatches.

### 6.3 Lines-of-code and edit-decision counts are partial

1,640 added / 33 removed, and 12 accepted edit decisions, are far below the
9,255 lines actually committed and the hundreds of file operations performed.
These metrics appear to count only a subset of edit paths — most likely
orchestrator-issued `Edit`/`Write` calls, not work done inside subagents. Useful
directionally; not a measure of output.

### 6.4 `claude_code_active_time_seconds_total` is wrong

Reports **247 s (4 min)** against roughly 2 h 35 m of active session time and
1 h 26 m of subagent wall clock measured independently. Whatever this metric
counts, it is not wall-clock activity for this run. **Do not use it.**

### 6.5 What is not captured at all

- **Per-task attribution.** No `task_id` on any series, so cost cannot be
  attributed to a task. `.claude/metrics/task-events.jsonl` has per-task outcomes
  but no cost. Joining the two would need the arm's gate timestamps as a bridge.
- **Escalation cost.** Not separable — an escalation dispatch is just another
  `subagent` series.
- **Wall clock per dispatch.** Only in `STATS.md`, from transcripts.

---

## 7. Identity and provenance

| Label | Value |
|---|---|
| `experiment_arm` | `go-job-processing-service` |
| `apparatus_sha` | `88abd63` |
| `cli_version` | `2.1.233` |
| `session_id` | `8738e7a0-2406-4009-88ab-8b76ac2efba6` |
| `terminal_type` | `xterm` |
| `os_type` / `host_arch` | `linux` / `arm64` |

Metrics present in the pipeline: `claude_code_cost_usage_USD_total`,
`claude_code_token_usage_tokens_total`, `claude_code_session_count_total`,
`claude_code_active_time_seconds_total`,
`claude_code_lines_of_code_count_total`,
`claude_code_code_edit_tool_decision_total`,
`claude_code_commit_count_total`.

Extraction queries used the form:

```promql
sum by (<dim>) (max_over_time(claude_code_<metric>{experiment_arm="<arm>"}[24h]))
```

---

## 8. The three findings worth carrying forward

1. **The orchestrator is the dominant cost.** 70 % of spend, ~3× all workers
   combined. Worker turn budgets and model tiers are not where the budget goes;
   orchestrator context length is. Any future cost control should start there.
2. **Prompt caching is doing the heavy lifting** — 85 % cache reads, 0.3 % fresh
   input. A design that fragmented context across many short sessions would cost
   far more for the same work.
3. **Escalation is cheap.** Opus-as-escalation was $0.50, under 12 % of the run,
   across three escalations including one direct-Opus task assignment. The policy's
   caution about Opus overuse is not borne out by the numbers — the expensive
   Opus is the orchestrator, which is unavoidable by design.
4. **The real budget is the subscription window, not the dollar.** ~1.5 of the
   5 h Pro windows went into this run, and the two multi-hour gaps in the timeline
   were rate-limit waits rather than work. $4.21 makes the run look nearly free;
   1.5 windows makes it look like most of a day's allowance. The second framing
   is the one that predicts whether the next arm can be finished in a sitting.
