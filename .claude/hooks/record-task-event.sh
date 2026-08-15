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

# Never let a recording problem affect the run: this hook always exits 0 (see
# the final line). An ERR trap is deliberately NOT used — a failing test command
# is a normal, expected outcome that must be recorded, and an `exit 0` trap here
# would abort before writing the record, silently losing every gate refusal.

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
TEST_OUTPUT=""
if [ -z "$(printf '%s' "$TEST_COMMAND" | tr -d ' \t\n')" ]; then
  OUTCOME="gate_refused_no_test_command"
else
  START="$(date +%s)"
  TEST_OUTPUT="$( cd "$PROJECT_DIR" && /usr/bin/env bash -c "$TEST_COMMAND" 2>&1 )"
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

# Experiment labels.
#
# Prefer the live environment, then fall back to deriving them from git the same
# way run-arm.sh does. The fallback is what makes attribution reliable: hook
# subprocesses do not inherit settings.json's env block, and both warmup arms
# were launched by running `claude` directly rather than through run-arm.sh, so
# every one of their records came out unlabelled. The branch is always available.
ARM="${OTEL_RESOURCE_ATTRIBUTES:-}"
if [ -z "$ARM" ]; then
  BRANCH="$(cd "$PROJECT_DIR" && git rev-parse --abbrev-ref HEAD 2>/dev/null || echo unknown)"
  ARM_NAME="${BRANCH#experiment/}"
  SHA="$(cd "$PROJECT_DIR" && git merge-base HEAD main 2>/dev/null | cut -c1-7 || true)"
  CLI_VER="$(grep -oE '^ARG CLAUDE_CODE_VERSION=.*' "$PROJECT_DIR/.claude/sandbox/Dockerfile" 2>/dev/null | cut -d= -f2 || true)"
  ARM="experiment.arm=${ARM_NAME},apparatus.sha=${SHA:-unknown},cli.version=${CLI_VER:-unknown}"
fi

# Build the record with python3 so that subjects containing quotes, newlines, or
# other JSON metacharacters cannot corrupt the line. Falls back to skipping the
# record rather than writing something malformed.
python3 - "$OUT_FILE" "$OUTCOME" "$ELAPSED" "$ARM" "$PAYLOAD" "$TEST_OUTPUT" <<'PY' 2>/dev/null || exit 0
import json, sys, datetime, re

out_file, outcome, elapsed, arm_raw, payload_raw, test_output = sys.argv[1:7]

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


def count_tests(out):
    """Best-effort count of tests actually executed.

    A suite that runs zero tests still exits 0, so `gate_passed` alone cannot
    distinguish a real pass from a vacuous one. On the Rust warmup arm the first
    two tasks passed the gate before any test existed. Returns None when the
    format is unrecognised — unknown is reported as unknown, never as zero.
    """
    if not out:
        return None
    total = None

    # Rust: "test result: ok. 12 passed; 0 failed; ..."
    m = re.findall(r"test result:\s*\w+\.\s*(\d+)\s+passed", out)
    if m:
        return sum(int(x) for x in m)

    # Go: "ok  pkg  0.01s" / "?  pkg  [no test files]" / "--- PASS: TestX"
    if re.search(r"\[no test files\]", out) or re.search(r"^(ok|FAIL|\?)\s", out, re.M):
        run = len(re.findall(r"^\s*--- (?:PASS|FAIL|SKIP):", out, re.M))
        if run:
            return run
        # Without -v there are no per-test lines, so count packages instead:
        # a package reporting "ok" ran at least one test, and one reporting
        # "[no test files]" ran none. Returning the count of test-bearing
        # packages keeps zero meaningful (the vacuous case) without claiming a
        # precise test count the output does not provide.
        ok_pkgs = len(re.findall(r"^ok\s+\S+", out, re.M))
        fail_pkgs = len(re.findall(r"^FAIL\s+\S+", out, re.M))
        if ok_pkgs or fail_pkgs:
            return ok_pkgs + fail_pkgs
        if re.search(r"\[no test files\]", out):
            return 0
        total = None

    # pytest / jest style: "12 passed"
    m = re.search(r"(\d+)\s+passed", out)
    if m:
        return int(m.group(1))
    return total


tests_run = count_tests(test_output)

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
    "cli_version": arm.get("cli.version"),
}

# A pass with zero tests is vacuous: the gate contributed no evidence. Recorded
# distinctly so the events file cannot be read as five meaningful passes when
# only three were.
if outcome == "gate_passed" and tests_run == 0:
    record["outcome"] = "gate_passed_vacuous"
record["tests_run"] = tests_run

if elapsed:
    record["test_elapsed_s"] = int(elapsed)

with open(out_file, "a") as f:
    f.write(json.dumps(record, ensure_ascii=False) + "\n")
PY

exit 0
