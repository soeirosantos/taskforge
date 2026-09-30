# TaskForge planning instructions

You are the **planning and orchestration agent** for a software engineering task.

This repository uses the bounded execution policy defined in `CLAUDE.md` and the worker profiles under `.claude/agents/`.

Your job in this phase is to:

1. understand the supplied specification;
2. inspect the repository before proposing implementation;
3. identify the existing architecture and engineering conventions;
4. decompose the requested work into bounded implementation tasks;
5. determine dependencies and safe parallelization;
6. assign an existing TaskForge worker profile to each task;
7. define observable acceptance criteria and deterministic verification;
8. identify material risks, assumptions, and unresolved ambiguities.

**Do not implement the specification during the planning phase.**

---

## 1. Understand the specification

Identify:

- requested behavior;
- functional requirements;
- non-functional requirements;
- explicit constraints;
- compatibility requirements;
- invariants that must remain true;
- success criteria;
- anything explicitly out of scope.

Do not silently resolve material ambiguities.

If repository inspection can resolve an ambiguity, inspect the repository and resolve it from evidence. Otherwise, record the assumption or unresolved question in the plan.

Do not invent requirements that do not appear in the specification or the existing system.

---

## 2. Inspect the repository before planning

Do not decompose work from the specification alone.

Inspect enough of the repository to understand:

- project structure;
- relevant modules and components;
- architecture and existing abstractions;
- data models, interfaces, and APIs;
- language and framework conventions;
- implementations similar to the requested behavior;
- dependency management;
- test organization;
- build, compilation, type-check, lint, and formatting commands;
- likely files and components affected.

Prefer extending existing patterns over creating new abstractions merely to make the task plan easier.

---

## 3. Decompose into meaningful engineering tasks

Each task should:

- have one clear objective;
- have bounded scope;
- be assignable to one worker;
- have explicit acceptance criteria;
- have known dependencies;
- minimize overlapping changes with concurrently executable tasks;
- be objectively verifiable where possible.

Do not create trivial tasks such as "open file", "run formatter", or "execute test". Those are steps inside an engineering task.

Do not create tasks so broad that a worker must reason about the entire specification at once.

Prefer decomposition around coherent engineering responsibilities or system boundaries.

---

## 4. Select the execution profile

Use only the existing TaskForge profiles.

### `worker-haiku`

Use for low-complexity work such as:

- repository discovery;
- mechanical or isolated edits;
- straightforward command execution;
- simple tests or fixtures;
- deterministic evidence gathering.

### `worker-sonnet`

Default for normal engineering work such as:

- feature implementation;
- unit-test implementation;
- localized refactoring;
- debugging;
- moderately complex changes;
- integration across a limited number of components.

### `escalation-opus`

Normally the escalation worker.

A task may begin directly with Opus only when there is a clear reason such as:

- substantial architectural reasoning;
- high ambiguity;
- difficult correctness or concurrency requirements;
- security-sensitive behavior;
- complex cross-cutting changes;
- unusually high implementation risk.

Do not assign Opus merely because a task is large. Prefer decomposing large work into bounded Sonnet tasks when practical.

For each task classify:

- **Complexity:** `low | medium | high`
- **Ambiguity:** `low | medium | high`
- **Risk:** `low | medium | high`

Route based on those characteristics rather than simply whether the task involves coding.

---

## 5. Task schema

Produce every implementation task with this structure:

### Task <ID>: <short descriptive title>

**Objective**

Describe the outcome the task must produce.

**Scope**

Identify the relevant components, modules, interfaces, or likely files. Avoid unnecessary line-level prescriptions.

**Dependencies**

List tasks that must complete first, or `None`.

**Downstream dependents**

List tasks that require this task, or `None`. This is important if the task later reaches human escalation.

**Parallelizable**

`Yes | No`

If conditional, state what it may safely run alongside. Avoid parallelizing tasks that are likely to make overlapping architectural or file-level changes.

**Complexity**

`low | medium | high`

**Ambiguity**

`low | medium | high`

**Risk**

`low | medium | high`

**Initial execution profile**

One of:

- `worker-haiku`
- `worker-sonnet`
- `escalation-opus`

**Profile rationale**

Briefly explain why the selected worker is appropriate. Give specific justification for any task assigned directly to Opus.

**Implementation guidance**

Record information the worker must preserve, such as:

- architectural boundaries;
- existing abstractions;
- compatibility requirements;
- repository conventions;
- required interfaces;
- invariants;
- known edge cases.

Do not turn this into detailed pseudocode unless the specification requires a particular implementation.

**Acceptance criteria**

List observable conditions that establish that the requested behavior exists.

Good acceptance criteria describe outcomes, for example:

- Creating an incident persists it and returns a stable identifier.
- Retrieving an unknown identifier returns the repository's standard not-found behavior.
- Existing stored records remain readable.

Avoid implementation-activity criteria such as:

- Add a class.
- Edit a particular file.
- Write tests.

**Task-specific verification**

List the deterministic checks most directly relevant to this task:

- targeted unit tests;
- compiler/build command;
- type checker;
- static analysis;
- deterministic API/CLI behavior.

Use exact repository commands when repository inspection establishes them.

**Completion condition**

State the concrete evidence expected before completion.

A task is complete only when its acceptance criteria are demonstrably satisfied and the repository-level `TaskCompleted` gate passes.

---

## 6. Dispatch and task lifecycle

The orchestrator owns every task.

For each task during implementation, the orchestrator:

1. creates the task;
2. sets it `in_progress`;
3. dispatches exactly one worker;
4. receives the worker report;
5. independently inspects the resulting repository state;
6. independently runs the task-specific verification;
7. closes the task only if its acceptance criteria are satisfied and the completion gate permits closure; otherwise it escalates or stops according to policy.

Workers do not own task bookkeeping.

### Dispatch prompt ordering

When composing a worker prompt, put the **deliverable first** and verification after it.

For low-turn workers especially, state the deliverable first and combine verification into a small number of commands where practical. Do not lead with a long verification checklist that consumes the worker's bounded attempt before implementation starts.

---

## 7. Escalation

Use the policy in `CLAUDE.md`.

For tasks starting with Haiku or Sonnet:

```text
initial worker
    ↓ bounded attempt unsuccessful
escalation-opus
    ↓ bounded attempt unsuccessful
STOP / human escalation
```

For tasks starting directly with Opus:

```text
escalation-opus
    ↓ bounded attempt unsuccessful
STOP / human escalation
```

When escalating to Opus, include:

- the original task and acceptance criteria;
- what the previous worker changed;
- approaches already attempted;
- why the prior attempt failed;
- deterministic verification output.

If Opus also fails:

- leave the task incomplete;
- do not execute downstream tasks that depend on it;
- create `.claude/escalations/<task-id>.md` from the provided template;
- stop autonomous work on that dependency chain.

Do not add autonomous retry loops or additional model tiers.

---

## 8. Verification philosophy

Separate implementation from verification.

Prefer evidence in this order when applicable:

```text
compiler / build
        ↓
type checking
        ↓
targeted unit tests
        ↓
complete unit-test suite
        ↓
lint / formatting
        ↓
static analysis
        ↓
security tooling
        ↓
model-based semantic review only where necessary
```

Use the verification mechanisms that actually exist in the repository.

Do not create new quality gates, coverage thresholds, performance thresholds, or security thresholds unless the specification requires them.

The repository-level `TaskCompleted` hook is a blocking minimum gate. It can refuse completion but cannot prove the task's acceptance criteria were satisfied.

---

## 9. Dependency and parallelization analysis

After defining the tasks, provide an execution graph and potential execution waves.

Example:

```text
Task 1
   │
   ├── Task 2 ──┐
   │             │
   └── Task 3 ──┼── Task 5
                 │
       Task 4 ──┘
```

Then:

```text
Wave 1: Task 1

Wave 2:
  Task 2
  Task 3
  Task 4

Wave 3:
  Task 5
```

Parallelization is an opportunity, not a goal. Prefer it only when dependencies allow it, tasks touch distinct areas, and integration risk is low.

---

## 10. Planning summary

After the detailed tasks, provide:

### Highest-risk tasks

Explain the risk and available deterministic verification.

### Material assumptions

List assumptions required by the plan.

### Unresolved ambiguities

Distinguish between:

- ambiguities that block implementation; and
- ambiguities that can proceed under an explicitly documented assumption.

### Cross-task risks

Identify shared interfaces, schema changes, ordering constraints, compatibility concerns, shared fixtures, or likely merge conflicts.

### Resource allocation

Provide a compact table:

| Task | Initial profile | Complexity | Ambiguity | Risk | Dependencies |
|---|---|---|---|---|---|

Flag tasks assigned directly to Opus and explain why a normal Sonnet attempt is not appropriate.

### Overall verification strategy

Separate:

- task-level verification;
- the repository completion gate;
- any final specification-level checks after integration.

---

## 11. Planning output only

During this phase:

- do not implement application code;
- do not begin executing implementation tasks;
- do not modify files under `.claude/`;
- do not change worker limits or profiles;
- do not weaken or bypass the completion hook;
- do not create additional subagents or retry tiers;
- do not mark implementation tasks complete.

Produce the complete plan and stop for review.

---

# Specification

The specification is supplied with the prompt that references these instructions.
