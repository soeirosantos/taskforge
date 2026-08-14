#!/usr/bin/env bash
#
# Records one line per task-completion attempt to
# .claude/metrics/task-events.jsonl.
#
# Runs as a second TaskCompleted hook, after the verification gate. Claude Code
# passes the hook payload (task_id, task_subject, ...) on stdin as JSON.
#
# This recorder is OBSERVATIONAL ONLY. It always exits 0, even when it cannot
# write, so that a recording failure can never block or unblock a task. The gate
# decides; this only takes notes. Because hooks in a matcher group run in order
# and their exit codes are evaluated independently, exiting 0 here does not
# override the gate's exit 2.
#
# It re-runs the configured test command to learn the current outcome, rather
# than reading the gate's exit code, which is not exposed to sibling hooks. For
# a fast unit-test suite this is cheap; if it ever is not, drop this hook and
# rely on the reconcile script alone.

set -uo pipefail

SCRIPT_DIR="$(cd "$(dirname "${BASH_SOURCE[0]}")" && pwd)"
PROJECT_DIR="${CLAUDE_PROJECT_DIR:-$(cd "$SCRIPT_DIR/../.." && pwd)}"
CONFIG_FILE="$SCRIPT_DIR/test-command.conf"
OUT_DIR="$PROJECT_DIR/.claude/metrics"
OUT_FILE="$OUT_DIR/task-events.jsonl"

# Never let a recording problem affect the run.
trap 'exit 0' ERR

PAYLOAD="$(cat 2>/dev/null || true)"

TEST_COMMAND=""
TEST_TIMEOUT_SECONDS=300
if [ -f "$CONFIG_FILE" ]; then
  # shellcheck disable=SC1090
  . "$CONFIG_FILE" 2>/dev/null || true
fi

# Re-run the suite to classify the outcome this attempt saw.
OUTCOME="unknown"
ELAPSED=""
if [ -z "$(printf '%s' "$TEST_COMMAND" | tr -d ' \t\n')" ]; then
  OUTCOME="gate_refused_no_test_command"
else
  START="$(date +%s)"
  ( cd "$PROJECT_DIR" && /usr/bin/env bash -c "$TEST_COMMAND" ) >/dev/null 2>&1
  STATUS=$?
  ELAPSED=$(( $(date +%s) - START ))
  if [ "$STATUS" -eq 0 ]; then
    OUTCOME="gate_passed"
  elif [ "$STATUS" -eq 127 ]; then
    OUTCOME="gate_refused_tests_unrunnable"
  else
    OUTCOME="gate_refused_tests_failed"
  fi
fi

mkdir -p "$OUT_DIR" 2>/dev/null || exit 0

# Experiment labels. Prefer the live environment, but fall back to reading
# settings.json directly: hook subprocesses do not reliably inherit the `env`
# block from settings, and an unlabelled record cannot be attributed to an arm.
ARM="${OTEL_RESOURCE_ATTRIBUTES:-}"
if [ -z "$ARM" ]; then
  ARM="$(python3 -c '
import json,sys
try:
    print(json.load(open(sys.argv[1]))["env"].get("OTEL_RESOURCE_ATTRIBUTES",""))
except Exception:
    print("")
' "$PROJECT_DIR/.claude/settings.json" 2>/dev/null || true)"
fi

# Build the record with python3 so that subjects containing quotes, newlines, or
# other JSON metacharacters cannot corrupt the line. Falls back to skipping the
# record rather than writing something malformed.
python3 - "$OUT_FILE" "$OUTCOME" "$ELAPSED" "$ARM" "$PAYLOAD" <<'PY' 2>/dev/null || exit 0
import json, sys, datetime

out_file, outcome, elapsed, arm_raw, payload_raw = sys.argv[1:6]

try:
    payload = json.loads(payload_raw) if payload_raw.strip() else {}
except Exception:
    payload = {}

# OTEL_RESOURCE_ATTRIBUTES is "k=v,k=v"; pull out the experiment labels.
arm = {}
for pair in arm_raw.split(","):
    if "=" in pair:
        k, v = pair.split("=", 1)
        arm[k.strip()] = v.strip()

record = {
    "ts": datetime.datetime.now(datetime.timezone.utc).isoformat(),
    "event": "completion_attempt",
    "outcome": outcome,
    "task_id": payload.get("task_id"),
    "task_subject": payload.get("task_subject"),
    "session_id": payload.get("session_id"),
    "cwd": payload.get("cwd"),
    "experiment_arm": arm.get("experiment.arm"),
    "apparatus_sha": arm.get("apparatus.sha"),
}
if elapsed:
    record["test_elapsed_s"] = int(elapsed)

with open(out_file, "a") as f:
    f.write(json.dumps(record, ensure_ascii=False) + "\n")
PY

exit 0
