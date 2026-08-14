# Human escalation record

Copy this file to `.claude/escalations/<task-id>.md` and fill it in when a task
has exhausted the bounded execution policy — that is, a normal worker failed the
completion gate **and** the single `escalation-opus` attempt also failed.

This record is informational only. It does not retry anything and it does not
change any state. The decision to escalate comes from the bounded execution
policy in `CLAUDE.md`, never from an agent deciding that it "feels stuck".

---

## Task ID

<!-- The Claude Code task ID. -->

## Task objective

<!-- What this task was supposed to accomplish, in plain language. -->

## Acceptance criteria

<!-- The criteria the task was given, verbatim. -->

## Worker/model attempts made

<!--
One row per bounded attempt, in order. Example:

| # | Worker          | Model  | Outcome                                   |
|---|-----------------|--------|-------------------------------------------|
| 1 | worker-sonnet   | sonnet | Exhausted maxTurns; gate not satisfied.   |
| 2 | escalation-opus | opus   | Diagnosed X; fix incomplete; gate failed. |
-->

| # | Worker | Model | Outcome |
|---|--------|-------|---------|
|   |        |       |         |

## Repository/files changed

<!-- Paths touched across all attempts, and the current state of each: kept, reverted, or left partially modified. -->

## Latest deterministic verification result

<!--
The actual output of the TaskCompleted gate on its most recent run:
the test command, the exit code, and the relevant failure output.
Paste real output. Do not summarize it away.
-->

```
```

## Unresolved failure

<!-- The specific condition that still blocks completion. Be concrete. -->

## Approaches already attempted

<!-- What was tried and why each did not work, so a human does not repeat it. -->

## Assumptions or decisions requiring human input

<!--
Ambiguities, missing requirements, architectural choices, or risk calls that an
autonomous agent should not make on its own.
-->

## Dependent tasks now blocked

<!-- Tasks that require this one and must not proceed. -->
