#!/usr/bin/env bash
#
# Blocking TaskCompleted gate.
#
# The gate runs the repository's configured complete unit-test suite:
#
#   pass / exit 0     -> task closure is not blocked
#   failure           -> exit 2; task remains incomplete
#   missing command   -> exit 2; task remains incomplete
#   timeout           -> terminate verification and exit 2
#
# Passing this gate is necessary, but it does not certify that the task's
# acceptance criteria were satisfied.

set -uo pipefail

TEST_COMMAND=""
TEST_TIMEOUT_SECONDS=300
MAX_STDERR_LINES=200

SCRIPT_DIR="$(cd "$(dirname "${BASH_SOURCE[0]}")" && pwd)"
PROJECT_DIR="${CLAUDE_PROJECT_DIR:-$(cd "$SCRIPT_DIR/../.." && pwd)}"
CONFIG_FILE="$SCRIPT_DIR/test-command.conf"

refuse() {
  echo "TASK COMPLETION REFUSED by $(basename "${BASH_SOURCE[0]}")" >&2
  echo "" >&2
  printf '%s\n' "$@" >&2
  exit 2
}

[ -d "$PROJECT_DIR" ] || refuse   "Project directory does not exist: $PROJECT_DIR"   "Verification could not run."

cd "$PROJECT_DIR" || refuse   "Could not enter project directory: $PROJECT_DIR"   "Verification could not run."

if [ -f "$CONFIG_FILE" ]; then
  # shellcheck disable=SC1090
  . "$CONFIG_FILE" || refuse     "Could not load verification configuration: $CONFIG_FILE"
fi

case "$TEST_TIMEOUT_SECONDS" in
  ''|*[!0-9]*|0)
    refuse       "Invalid TEST_TIMEOUT_SECONDS: '$TEST_TIMEOUT_SECONDS'"       "Set a positive integer in $CONFIG_FILE." ;;
esac

TRIMMED_COMMAND="$(printf '%s' "$TEST_COMMAND" | tr -d ' \t\n')"
if [ -z "$TRIMMED_COMMAND" ]; then
  refuse     "No unit-test command is configured."     ""     "TaskForge fails closed when deterministic verification is unavailable."     "Set TEST_COMMAND in:"     "  $CONFIG_FILE"
fi

LOG_FILE="$(mktemp "${TMPDIR:-/tmp}/taskforge-verify-XXXXXX")" || refuse   "Could not create a temporary file for verification output."

# shellcheck disable=SC2064
trap "rm -f '$LOG_FILE'" EXIT

START_TS="$(date +%s)"

# Run the suite in its own process group where supported so timeout cleanup can
# terminate the test runner and its children.
set -m 2>/dev/null
/usr/bin/env bash -c "$TEST_COMMAND" >"$LOG_FILE" 2>&1 &
TEST_PID=$!
set +m 2>/dev/null

(
  waited=0
  while [ "$waited" -lt "$TEST_TIMEOUT_SECONDS" ]; do
    kill -0 "$TEST_PID" 2>/dev/null || exit 0
    sleep 1
    waited=$((waited + 1))
  done

  kill -TERM "-$TEST_PID" 2>/dev/null || kill -TERM "$TEST_PID" 2>/dev/null
  sleep 5
  kill -KILL "-$TEST_PID" 2>/dev/null || kill -KILL "$TEST_PID" 2>/dev/null
) &
WATCHDOG_PID=$!

wait "$TEST_PID" 2>/dev/null
TEST_STATUS=$?

kill "$WATCHDOG_PID" 2>/dev/null
wait "$WATCHDOG_PID" 2>/dev/null

ELAPSED=$(( $(date +%s) - START_TS ))

if [ "$TEST_STATUS" -ne 0 ] && [ "$ELAPSED" -ge "$TEST_TIMEOUT_SECONDS" ]; then
  refuse     "Verification exceeded its ${TEST_TIMEOUT_SECONDS}s timeout and was terminated."     "Command: $TEST_COMMAND"     "Elapsed: ${ELAPSED}s"     ""     "--- last ${MAX_STDERR_LINES} lines of output ---"     "$(tail -n "$MAX_STDERR_LINES" "$LOG_FILE" 2>/dev/null)"
fi

if [ "$TEST_STATUS" -eq 127 ]; then
  refuse     "The verification command could not be executed (exit 127)."     "Command: $TEST_COMMAND"     ""     "--- output ---"     "$(tail -n "$MAX_STDERR_LINES" "$LOG_FILE" 2>/dev/null)"
fi

if [ "$TEST_STATUS" -ne 0 ]; then
  refuse     "The unit-test suite failed (exit ${TEST_STATUS})."     "Command: $TEST_COMMAND"     "Elapsed: ${ELAPSED}s"     ""     "Fix the implementation or the legitimate test failure; do not weaken the gate."     ""     "--- last ${MAX_STDERR_LINES} lines of output ---"     "$(tail -n "$MAX_STDERR_LINES" "$LOG_FILE" 2>/dev/null)"
fi

echo "TaskForge gate satisfied: unit-test suite passed in ${ELAPSED}s."
echo "Command: $TEST_COMMAND"
echo "The gate does not certify that task acceptance criteria were satisfied."
exit 0
