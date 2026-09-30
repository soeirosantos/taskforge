# TaskForge

TaskForge is a small, repository-local execution policy for bounded agentic software development with Claude Code.

It is intentionally **not** an agent platform. Claude Code provides the runtime, task system, subagents, tools, and hooks. TaskForge adds an opinionated workflow around those primitives:

- inspect the repository before planning;
- decompose specifications into bounded, dependency-aware tasks;
- route work to a suitable worker profile;
- keep workers from recursively delegating;
- require explicit acceptance criteria;
- verify completion with deterministic repository checks;
- allow one bounded Opus escalation after a normal worker fails;
- stop for human input when autonomous execution is exhausted.

## What to copy

The reusable harness is:

```text
.claude/
  TASK_PLANNING_INSTRUCTIONS.md
  agents/
    worker-haiku.md
    worker-sonnet.md
    escalation-opus.md
  escalations/
    TEMPLATE.md
  hooks/
    test-command.conf
    verify-unit-tests.sh
  settings.json

CLAUDE.md
```

There is no metrics stack, experiment-arm machinery, sandbox, custom scheduler, or orchestration service.

## Add TaskForge to a new repository

Clone TaskForge somewhere outside the target repository.

For a repository that does **not** already contain `.claude/` or `CLAUDE.md`, copy the harness with a guard so existing Claude configuration cannot be overwritten accidentally:

```bash
git clone https://github.com/soeirosantos/taskforge.git

TARGET=/path/to/your-project

if [ -e "$TARGET/.claude" ] || [ -e "$TARGET/CLAUDE.md" ]; then
  echo "Existing Claude configuration detected. Merge TaskForge manually instead of copying."
  exit 1
fi

cp -R taskforge/.claude "$TARGET/"
cp taskforge/CLAUDE.md "$TARGET/"
```

If the target repository already has Claude configuration, copy the TaskForge files to a temporary location and merge `.claude/settings.json` and `CLAUDE.md` deliberately. Do not overwrite existing hooks, permissions, environment settings, or project instructions.

Make the verification hook executable if your copy process does not preserve file mode:

```bash
chmod +x .claude/hooks/verify-unit-tests.sh
```

## Configure verification

Edit:

```text
.claude/hooks/test-command.conf
```

Set `TEST_COMMAND` to the command that runs the repository's complete unit-test suite, and set a reasonable timeout.

Examples:

```bash
TEST_COMMAND="go test ./..."
TEST_COMMAND="cargo test"
TEST_COMMAND="pytest"
TEST_COMMAND="npm test"
```

The gate fails closed when no test command is configured. A task cannot be closed merely because an agent says it is done.

## Task-tool availability

TaskForge's orchestration policy depends on Claude Code's `TaskCreate`, `TaskUpdate`, `TaskGet`, and `TaskList` tools. The included `.claude/settings.json` sets:

```text
CLAUDE_CODE_ENABLE_TODO_TOOLS=1
```

This keeps the task-tracking tools available on newer Claude model families where Claude Code may not expose them by default. If the orchestrator still starts without `TaskCreate` / `TaskUpdate`, stop rather than silently falling back to untracked work.

## Use it

### 1. Plan

Start Claude Code in the target repository and provide the specification with:

```text
Follow .claude/TASK_PLANNING_INSTRUCTIONS.md.

The specification is:
<your specification>
```

The planning phase inspects the repository, produces bounded implementation tasks, assigns worker profiles, identifies dependencies and safe parallelization, defines acceptance criteria and verification, and then stops.

Review the plan before implementation.

### 2. Implement

After reviewing the plan:

```text
Begin implementation using the approved plan and the execution policy in CLAUDE.md.
```

The orchestrator owns task creation, dispatch, verification, completion, and escalation.

## Execution policy

Normal flow:

```text
worker-haiku or worker-sonnet
        ↓ bounded attempt unsuccessful
escalation-opus
        ↓ bounded attempt unsuccessful
STOP / human escalation
```

A task is complete only when:

1. its acceptance criteria are demonstrably satisfied; and
2. the blocking `TaskCompleted` verification gate passes.

Passing tests are necessary evidence, not proof that the requested behavior was implemented correctly.
