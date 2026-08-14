# taskforge

## How to run an experiment arm

### 1. Build the sandbox image (once)

```bash
cd .claude/sandbox
docker compose build
cd ../..
```

Rebuild only when the `Dockerfile` changes.

### 2. Start the metrics stack

```bash
cd .claude/metrics
docker compose up -d
cd ../..
```

This must run **before** the sandbox: the sandbox joins the `metrics_default`
network, and `run-arm.sh` refuses to launch if that network does not exist.

- Prometheus — <http://localhost:9090>
- Grafana — <http://localhost:3000> (`admin` / `admin`)

### 3. Prepare the branch

Each arm runs on its own branch, and `main` stays free of application code.

```bash
git checkout main
git checkout -b experiment/go
```

Set the test command for the language in `.claude/hooks/test-command.conf`:

```bash
TEST_COMMAND="go test ./..."     # experiment/go
TEST_COMMAND="cargo test"        # experiment/rust
```

Commit it. This is the only file expected to differ between arms —
`verify-unit-tests.sh` must stay identical everywhere, and `run-arm.sh` refuses
to launch if it does not.

### 4. Launch the sandbox

```bash
export ANTHROPIC_API_KEY=sk-ant-...
.claude/sandbox/run-arm.sh
```

This opens an interactive shell inside the container. `run-arm.sh` derives
`experiment.arm` from the branch name and `apparatus.sha` from the merge-base
with `main`, and refuses to start if the arm is misconfigured.

Inside the container:

```bash
claude --dangerously-skip-permissions
```

The flag is reasonable here because the container mounts the repository and
nothing else — no host home directory, no SSH keys, no other projects.

### 5. Plan, then implement

Planning and implementation are separate phases, and planning is interactive:
the agent will ask follow-up questions about the specification, and the plan is
reviewed before any implementation starts.

In the Claude Code session, point the agent at the planning instructions and the
specification:

```
Follow .claude/TASK_PLANNING_INSTRUCTIONS.md.
The specification is in <path/to/spec.md>.
```

The planning phase produces a task decomposition and stops. Review it, then tell
the agent to begin implementation.

### 6. Reconcile before switching branches

```bash
.claude/metrics/reconcile-tasks.sh
```

Records the final state of every task, including tasks that were never attempted
because a worker exhausted its turn limit — those are invisible to the
`TaskCompleted` hook.

Run this **before** switching branches. The task store lives in `~/.claude/tasks/`
and is not branch-scoped, so reconciling late risks mixing arms.

Commit `.claude/metrics/task-events.jsonl` on the arm's branch. It is the
experiment result and travels with the branch that produced it.

---

## Headless runs

For a run that needs no interaction:

```bash
.claude/sandbox/run-arm.sh -p "<prompt>"
```

Everything after `run-arm.sh` is passed through to `claude`. Planning normally
needs interaction, so this is mainly useful for smoke tests.

---

## Troubleshooting

| Symptom | Cause |
| --- | --- |
| `ANTHROPIC_API_KEY is not set` | Export it in the shell that runs `run-arm.sh`. |
| `Network 'metrics_default' not found` | Start the metrics stack first (step 2). |
| `TEST_COMMAND is empty` | Set it in `.claude/hooks/test-command.conf` (step 3). |
| `verify-unit-tests.sh differs from main` | Per-branch config belongs in `test-command.conf`, not the gate script. |
| `WARNING: Prometheus is not responding` | Metrics stack is down; the run still works but records no metrics. |
| Every task refused by the gate | `TEST_COMMAND` unset, or the suite genuinely fails. The gate fails closed by design. |
