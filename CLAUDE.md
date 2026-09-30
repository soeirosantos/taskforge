# TaskForge agent execution policy

This repository uses TaskForge for bounded agentic implementation.

## Task lifecycle

- Planned implementation units must be represented as Claude Code tasks (`TaskCreate` / `TaskUpdate`), not informal todos.
- The **orchestrator owns the task lifecycle**. It creates the task, sets it `in_progress`, dispatches exactly one worker, receives the result, independently runs verification, and then closes or escalates the task.
- Worker subagents must not create, update, or close tasks.
- A worker's statement that work is complete is not sufficient evidence for completion.
- If the orchestrator does not have the task tools required by this policy, do not silently substitute untracked work. Stop and report that the session must be started with task support available.

## Delegation

- The orchestrator owns delegation.
- Workers must never delegate further. The worker profiles under `.claude/agents/` deny the `Agent` tool.
- Use `worker-sonnet` for normal implementation.
- Use `worker-haiku` for low-complexity discovery, mechanical edits, command execution, and straightforward evidence gathering.
- A planning phase may assign a task directly to `escalation-opus` only when substantial architecture, ambiguity, correctness, security, or implementation risk clearly justifies it.

## Bounded execution and escalation

For tasks that begin with Haiku or Sonnet:

```text
initial worker
    ↓ bounded attempt unsuccessful
escalation-opus
    ↓ bounded attempt unsuccessful
STOP / human escalation
```

For a task assigned directly to Opus:

```text
escalation-opus
    ↓ bounded attempt unsuccessful
STOP / human escalation
```

Rules:

- A failed worker attempt does not complete the task.
- There is at most one Opus escalation attempt.
- There is no autonomous model tier after Opus and no recursive delegation.
- Before dispatching Opus after a failed attempt, the orchestrator must include the previous approach, what failed, and the deterministic verification output so the escalation does not start without context.
- If Opus also fails, autonomous work on that task stops.
- Tasks that depend on the failed task must not proceed.
- The orchestrator records the unresolved task using `.claude/escalations/TEMPLATE.md`.

## Definition of complete

A task is complete only when:

1. its acceptance criteria are demonstrably satisfied; and
2. the repository verification gate passes.

The blocking `TaskCompleted` hook is configured in `.claude/settings.json` and runs `.claude/hooks/verify-unit-tests.sh`.

The gate fails closed when the test command is missing, cannot run, fails, or times out.

A passing gate is a **necessary condition, not a certification**. It means the configured checks found nothing that blocks completion. The orchestrator must still confirm that the task's acceptance criteria are satisfied.

Do not weaken, skip, delete, or modify tests or verification configuration merely to close a task.

## Verification ownership

After a worker returns, the orchestrator runs the relevant deterministic verification itself rather than trusting the worker report.

Prefer deterministic evidence when available:

```text
build / compile
    ↓
type checking
    ↓
targeted tests
    ↓
complete unit-test suite
    ↓
lint / formatting
    ↓
static analysis / security checks
    ↓
model-based semantic review only where deterministic checks cannot answer
```

Not every repository has every layer. Use the checks that actually exist.

## Harness protection

Everything under `.claude/` is harness configuration.

Implementation tasks must not modify TaskForge worker profiles, hooks, settings, planning instructions, or escalation policy unless the user explicitly asks to change the harness itself.
