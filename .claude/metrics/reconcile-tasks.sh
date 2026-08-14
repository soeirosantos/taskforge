#!/usr/bin/env bash
#
# Reconciles the task store against recorded completion attempts, and appends
# one final-state record per task to task-events.jsonl.
#
# Why this exists: the TaskCompleted hook only fires when an agent ATTEMPTS
# completion. A task that exhausts maxTurns and gives up never fires it. Those
# tasks are invisible to the hook but are exactly the "lost or spinning" failure
# this experiment is measuring, so they have to be recovered from the task store
# after a run.
#
# Run this at the end of an experiment arm, before switching branches:
#
#     .claude/metrics/reconcile-tasks.sh
#
# Read-only with respect to the task store. Safe to run repeatedly; each run
# appends a new reconciliation batch stamped with its own timestamp.

set -uo pipefail

SCRIPT_DIR="$(cd "$(dirname "${BASH_SOURCE[0]}")" && pwd)"
PROJECT_DIR="${CLAUDE_PROJECT_DIR:-$(cd "$SCRIPT_DIR/../.." && pwd)}"
OUT_FILE="$PROJECT_DIR/.claude/metrics/task-events.jsonl"
TASK_STORE="${CLAUDE_TASK_STORE:-$HOME/.claude/tasks}"

if [ ! -d "$TASK_STORE" ]; then
  echo "No task store found at $TASK_STORE" >&2
  echo "Nothing to reconcile. If tasks ran on another machine or under a" >&2
  echo "different HOME, set CLAUDE_TASK_STORE and re-run." >&2
  exit 1
fi

python3 - "$TASK_STORE" "$OUT_FILE" "${OTEL_RESOURCE_ATTRIBUTES:-}" <<'PY'
import json, sys, glob, os, datetime

store, out_file, arm_raw = sys.argv[1:4]

arm = {}
for pair in arm_raw.split(","):
    if "=" in pair:
        k, v = pair.split("=", 1)
        arm[k.strip()] = v.strip()

# Which tasks already have a recorded completion attempt?
attempted = set()
if os.path.exists(out_file):
    with open(out_file) as f:
        for line in f:
            try:
                rec = json.loads(line)
            except Exception:
                continue
            if rec.get("event") == "completion_attempt" and rec.get("task_id"):
                attempted.add(str(rec["task_id"]))

now = datetime.datetime.now(datetime.timezone.utc).isoformat()
rows, written = [], 0

for path in sorted(glob.glob(os.path.join(store, "*", "*.json"))):
    try:
        task = json.load(open(path))
    except Exception:
        continue
    tid = str(task.get("id", os.path.basename(path).removesuffix(".json")))
    status = task.get("status")

    # A task still in_progress that never attempted completion is the silent
    # exhaustion case: the worker ran out of turns without reaching the gate.
    if status == "in_progress" and tid not in attempted:
        classification = "abandoned_no_attempt"
    elif status == "completed":
        classification = "completed"
    elif status == "in_progress":
        classification = "in_progress_after_refusal"
    else:
        classification = str(status)

    rows.append((tid, task.get("subject"), status, classification,
                 task.get("blockedBy") or [], tid in attempted))

    with open(out_file, "a") as f:
        f.write(json.dumps({
            "ts": now,
            "event": "final_state",
            "task_id": tid,
            "task_subject": task.get("subject"),
            "status": status,
            "classification": classification,
            "blocked_by": task.get("blockedBy") or [],
            "blocks": task.get("blocks") or [],
            "had_completion_attempt": tid in attempted,
            "session_dir": os.path.basename(os.path.dirname(path)),
            "experiment_arm": arm.get("experiment.arm"),
            "apparatus_sha": arm.get("apparatus.sha"),
        }, ensure_ascii=False) + "\n")
    written += 1

print(f"Reconciled {written} task(s) from {store}")
print(f"Appended final_state records to {out_file}\n")

if rows:
    print(f"{'TASK':<6} {'STATUS':<14} {'CLASSIFICATION':<28} {'ATTEMPTED':<10} SUBJECT")
    for tid, subject, status, classification, _, was in sorted(rows, key=lambda r: r[0]):
        print(f"{tid:<6} {str(status):<14} {classification:<28} "
              f"{'yes' if was else 'NO':<10} {(subject or '')[:44]}")

    abandoned = [r for r in rows if r[3] == "abandoned_no_attempt"]
    if abandoned:
        print(f"\n{len(abandoned)} task(s) never attempted completion — worker likely")
        print("exhausted its turn limit without reaching the gate. These are")
        print("invisible to the TaskCompleted hook and are the reason this")
        print("reconciliation step exists.")
PY
