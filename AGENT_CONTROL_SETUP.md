Prepare this repository for **bounded agentic execution**.

This is an environment/bootstrap task only.

Do **not** implement the application specification, create application features, or begin product development.

The goal is to establish minimal deterministic controls so that later planning and implementation can use Claude Code subagents without allowing individual workers to run indefinitely.

## Objectives

Configure the repository with:

1. bounded Haiku, Sonnet, and Opus worker profiles;
2. deterministic task completion based on unit tests;
3. prevention of recursive subagent delegation;
4. a simple escalation policy;
5. a repository location for human-escalation records;
6. project instructions requiring later work to use these controls.

Keep this minimal. Do not build a custom orchestration framework.

---

## 1. Inspect existing configuration first

Before changing anything:

* inspect existing `.claude/` configuration;
* inspect any existing `CLAUDE.md`;
* inspect the repository language, build system, and test configuration;
* determine the correct command for running the complete unit-test suite;
* preserve and merge existing configuration rather than overwriting it.

If the repository does not yet have a runnable unit-test suite, still create the verification mechanism, but configure it to **fail closed** with a clear message until a real test command exists.

Do not allow "no test command configured" to count as successful verification.

Use only features supported by the installed Claude Code version.

If any required feature below is unavailable in the installed version, stop the bootstrap and report exactly which feature is unavailable rather than silently approximating it.

---

## 2. Create bounded execution profiles

Create these project-level subagents under:

`.claude/agents/`

### `worker-haiku`

Purpose:

* repository discovery;
* searches;
* simple mechanical tasks;
* running commands;
* gathering or summarizing deterministic evidence;
* other low-complexity work.

Configuration:

* model: `haiku`
* effort: `low`
* maxTurns: `8`

The worker must not spawn other agents.

Explicitly deny the `Agent` tool so nested delegation cannot occur.

Its instructions should state that it:

* executes only the task assigned by the parent;
* must not expand its scope;
* must not delegate;
* should return its result or failure when its bounded attempt ends.

---

### `worker-sonnet`

Purpose:

* normal software implementation;
* tests;
* localized refactoring;
* debugging;
* moderately complex engineering tasks.

Configuration:

* model: `sonnet`
* effort: `high`
* maxTurns: `15`

The worker must not spawn other agents.

Explicitly deny the `Agent` tool.

Its instructions should state that it:

* executes only its assigned bounded task;
* follows the supplied acceptance criteria;
* does not create additional subagents;
* does not declare success merely because implementation appears correct;
* relies on the external completion gate to determine whether the task can close.

---

### `escalation-opus`

Purpose:

* one bounded escalation attempt after a normal implementation worker cannot complete a task;
* diagnosing why the previous attempt failed;
* resolving difficult, ambiguous, architectural, or high-risk implementation problems.

Configuration:

* model: `opus`
* effort: `high`
* maxTurns: `15`

The worker must not spawn other agents.

Explicitly deny the `Agent` tool.

Its instructions should state that it must:

* review the original task and acceptance criteria;
* inspect the previous implementation state;
* inspect deterministic verification failures;
* diagnose the previous failure rather than blindly repeating the same approach;
* make one bounded attempt to resolve the task;
* stop and report the unresolved condition if the completion gate still cannot be satisfied.

Do not create additional escalation tiers.

---

## 3. Create a deterministic task-completion gate

Create a project-level `TaskCompleted` hook.

Place the executable verification logic under:

`.claude/hooks/`

Use an appropriate script format for the current operating system.

The hook must run the repository's complete unit-test suite.

Behavior:

```text
unit tests pass
    → exit successfully
    → task may be completed

unit tests fail
    → write useful failure information to stderr
    → exit 2
    → task must remain incomplete

tests cannot be executed
    → exit 2
    → task must remain incomplete

test execution exceeds its allowed runtime
    → terminate the test execution
    → exit 2
    → task must remain incomplete
```

The test timeout must be enforced by the verification script or test runner itself.

Do not rely solely on the Claude Code hook handler's own `timeout` setting as the completion safety mechanism.

Choose a reasonable test-execution timeout based on the existing repository. If there is not enough information to choose one intelligently, use a conservative initial value and document it clearly so it can be adjusted later.

The verification script must not modify application code.

---

## 4. Register the completion hook

Merge the `TaskCompleted` hook into:

`.claude/settings.json`

Do not overwrite unrelated existing settings or hooks.

Use `${CLAUDE_PROJECT_DIR}` when referencing project scripts where appropriate.

The completion gate must be a blocking synchronous command hook.

Do not configure this hook as asynchronous.

---

## 5. Require use of Claude Code tasks

The `TaskCompleted` gate only protects work that participates in Claude Code's task lifecycle.

Add or update a clearly delimited section in the project's `CLAUDE.md` establishing the following execution policy for future implementation work:

### Agent execution safety policy

* Planned implementation units must be represented as Claude Code tasks.
* Tasks must remain `in_progress` while implementation is incomplete.
* A task may be marked complete only through the normal task-completion mechanism so the `TaskCompleted` hook executes.
* An agent's textual claim that work is finished is not sufficient to consider a task complete.
* The main/orchestrating agent owns delegation.
* Worker subagents must never delegate further.
* Normal implementation should use `worker-sonnet` unless the planning phase selects another worker for a justified reason.
* Mechanical/simple work may use `worker-haiku`.
* If a normal worker exhausts its bounded attempt without satisfying the completion gate, the task remains incomplete.
* A failed normal implementation task may receive one bounded attempt using `escalation-opus`.
* If the Opus escalation attempt also fails, autonomous implementation of that task stops.
* Dependent tasks that require the failed task must not proceed.
* Human escalation is then required.

Do not add unrelated development instructions to `CLAUDE.md`.

---

## 6. Human escalation records

Create:

`.claude/escalations/`

Provide a minimal template for unresolved tasks.

When a task reaches human escalation, the orchestrator should later create a record containing:

* task ID;
* task objective;
* acceptance criteria;
* worker/model attempts made;
* repository/files changed;
* latest deterministic verification result;
* unresolved failure;
* approaches already attempted;
* assumptions or decisions requiring human input.

This record is informational.

The decision to escalate must come from the bounded execution policy, not from an agent deciding that it "feels stuck."

Do not implement an automated retry counter or custom state machine for this initial version.

---

## 7. Escalation policy

Prepare the environment for this simple policy:

```text
low-complexity task
    → worker-haiku
    → failure/exhaustion
    → escalation-opus
    → failure/exhaustion
    → STOP / human

normal implementation task
    → worker-sonnet
    → failure/exhaustion
    → escalation-opus
    → failure/exhaustion
    → STOP / human
```

A planning phase may assign a task directly to Opus when its complexity, ambiguity, or risk clearly warrants it.

There must be no autonomous model tier after Opus.

There must be no recursive worker delegation.

---

## 8. Do not add unnecessary controls

For this initial experiment, do NOT add:

* code-coverage thresholds;
* security severity gates;
* integration-test requirements;
* performance gates;
* automated retry counters;
* model-based completion checks;
* custom orchestration services;
* Agent SDK wrappers;
* CI/CD changes;
* elaborate task-state persistence.

Those may be evaluated later.

For now, the minimum definition of complete is:

**the task's acceptance criteria are satisfied and the repository's unit-test suite passes.**

---

## 9. Validate the bootstrap

After creating the configuration:

1. validate the syntax of `.claude/settings.json`;
2. validate all subagent frontmatter;
3. confirm the three subagents are discoverable by Claude Code;
4. confirm each has the intended model, effort, and `maxTurns`;
5. confirm workers cannot use the `Agent` tool;
6. confirm the `TaskCompleted` hook is registered;
7. run the verification script directly and confirm its exit behavior;
8. if practical, perform a harmless test demonstrating that a failing verification command prevents a task from being completed;
9. restore any temporary modifications made solely for validation.

Do not modify application behavior simply to test the bootstrap.

If creating `.claude/agents/` for the first time requires restarting Claude Code before the new agents are discoverable, tell me explicitly.

---

## 10. Final report

When finished, do not begin any application work.

Report only:

* files created;
* existing files modified;
* worker profiles and their limits;
* detected unit-test command;
* configured test timeout;
* completion-hook behavior;
* whether validation succeeded;
* anything I need to do manually, such as restarting Claude Code.

Then stop and wait for the planning specification.
