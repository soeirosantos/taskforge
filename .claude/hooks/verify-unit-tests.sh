#!/usr/bin/env bash
#
# Deterministic task-completion gate for this repository.
#
# Registered as a blocking, synchronous `TaskCompleted` hook in
# .claude/settings.json. It runs the repository's complete unit-test suite and
# can refuse to let a task close. It is a necessary condition for completion,
# never a sufficient one: satisfying the acceptance criteria is the other half,
# and this script cannot see that.
#
#   tests pass                  -> exit 0  (completion not blocked; NOT a certification
#                                           that the acceptance criteria were met)
#   tests fail                  -> failure info to stderr, exit 2 (task stays open)
#   tests cannot be executed    -> exit 2  (task stays open)
#   tests exceed the timeout    -> test execution is terminated, exit 2
#
# This script NEVER modifies application code. It only reads the repository and
# runs the configured test command.
#
# Written for bash 3.2 (the macOS system bash), with no dependency on GNU
# coreutils `timeout` / `gtimeout`, which are not present on a stock macOS.

set -uo pipefail

# =============================================================================
# CONFIGURATION
# =============================================================================
#
# This script holds NO per-language configuration, so that it stays byte-for-byte
# identical across every experiment branch. All values that vary by language live
# in:
#
#     .claude/hooks/test-command.conf
#
# `git diff main..experiment/<lang> -- .claude/hooks/` should show that config
# file and nothing else. If it shows changes to this script, the arms are no
# longer running the same apparatus and their results are not comparable.
#
# Defaults below apply when the config file omits a value. TEST_COMMAND has no
# default on purpose: an unconfigured gate must fail closed.

TEST_COMMAND=""

# Maximum wall-clock time the suite may run before it is terminated and the task
# is refused. 300s is a conservative placeholder, not a measured value; override
# it per branch once a real suite exists. Keep it strictly below the `timeout`
# configured for this hook in .claude/settings.json, so that this script — not
# the Claude Code hook handler — is what enforces the limit.
TEST_TIMEOUT_SECONDS=300

# How many trailing lines of test output to surface on failure.
MAX_STDERR_LINES=200

# =============================================================================
# LOGIC
# =============================================================================

SCRIPT_DIR="$(cd "$(dirname "${BASH_SOURCE[0]}")" && pwd)"
PROJECT_DIR="${CLAUDE_PROJECT_DIR:-$(cd "$SCRIPT_DIR/../.." && pwd)}"
CONFIG_FILE="$SCRIPT_DIR/test-command.conf"

# Refuse to complete the task. Everything that is not a clean, timely, passing
# test run lands here.
refuse() {
  echo "TASK COMPLETION REFUSED by $(basename "${BASH_SOURCE[0]}")" >&2
  echo "" >&2
  printf '%s\n' "$@" >&2
  exit 2
}

if [ ! -d "$PROJECT_DIR" ]; then
  refuse "Project directory does not exist: $PROJECT_DIR" \
         "The verification gate could not run, so the task must remain incomplete."
fi

cd "$PROJECT_DIR" || refuse \
  "Could not enter project directory: $PROJECT_DIR" \
  "The verification gate could not run, so the task must remain incomplete."

# --- Load the per-branch configuration ---------------------------------------
# A config file that exists but cannot be read or parsed is a verification
# failure, not a reason to fall back to defaults.
if [ -f "$CONFIG_FILE" ]; then
  # shellcheck disable=SC1090
  . "$CONFIG_FILE" || refuse \
    "The gate configuration file could not be loaded: $CONFIG_FILE" \
    "The verification gate could not run, so the task must remain incomplete."
fi

case "$TEST_TIMEOUT_SECONDS" in
  ''|*[!0-9]*|0)
    refuse \
      "Invalid TEST_TIMEOUT_SECONDS: '${TEST_TIMEOUT_SECONDS}'" \
      "Must be a positive integer. Fix it in $CONFIG_FILE." ;;
esac

# --- Fail closed when no test command is configured --------------------------
TRIMMED_COMMAND="$(printf '%s' "$TEST_COMMAND" | tr -d ' \t\n')"
if [ -z "$TRIMMED_COMMAND" ]; then
  refuse \
    "No unit-test command is configured, so completion cannot be verified." \
    "" \
    "This gate fails closed by design: an unconfigured test command is a" \
    "verification failure, not a pass." \
    "" \
    "To fix: set TEST_COMMAND in" \
    "  $CONFIG_FILE" \
    "to the command that runs this branch's complete unit-test suite, and set" \
    "TEST_TIMEOUT_SECONDS to a value measured from a real run." \
    "" \
    "Do NOT edit verify-unit-tests.sh to configure this. That script is shared" \
    "across all experiment branches and must stay identical in every one."
fi

# --- Run the suite under a self-enforced timeout -----------------------------
LOG_FILE="$(mktemp "${TMPDIR:-/tmp}/taskforge-verify-XXXXXX")" || refuse \
  "Could not create a temporary file to capture test output." \
  "The verification gate could not run, so the task must remain incomplete."
# shellcheck disable=SC2064
trap "rm -f '$LOG_FILE'" EXIT

START_TS="$(date +%s)"

# Job control puts the test suite in its own process group, so the watchdog can
# terminate the whole tree (test runner + any children it spawned), not just the
# top-level process. If job control is unavailable, we fall back to signalling
# the single pid.
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

# 2>/dev/null suppresses only bash's own "Terminated: 15" job notification; all
# real test output was already captured into $LOG_FILE.
wait "$TEST_PID" 2>/dev/null
TEST_STATUS=$?

kill "$WATCHDOG_PID" 2>/dev/null
wait "$WATCHDOG_PID" 2>/dev/null

ELAPSED=$(( $(date +%s) - START_TS ))

# --- Decide ------------------------------------------------------------------
if [ "$TEST_STATUS" -ne 0 ] && [ "$ELAPSED" -ge "$TEST_TIMEOUT_SECONDS" ]; then
  refuse \
    "Unit-test execution exceeded its ${TEST_TIMEOUT_SECONDS}s timeout and was terminated." \
    "Command: $TEST_COMMAND" \
    "Elapsed: ${ELAPSED}s" \
    "" \
    "A suite that does not finish is not a passing suite. Either the tests hang," \
    "or TEST_TIMEOUT_SECONDS in .claude/hooks/verify-unit-tests.sh is too low for" \
    "this repository. Investigate before raising the timeout." \
    "" \
    "--- last ${MAX_STDERR_LINES} lines of output before termination ---" \
    "$(tail -n "$MAX_STDERR_LINES" "$LOG_FILE" 2>/dev/null)"
fi

if [ "$TEST_STATUS" -eq 127 ]; then
  refuse \
    "The unit-test command could not be executed (exit 127: command not found)." \
    "Command: $TEST_COMMAND" \
    "" \
    "Dependencies may be uninstalled, or TEST_COMMAND may be wrong." \
    "" \
    "--- output ---" \
    "$(tail -n "$MAX_STDERR_LINES" "$LOG_FILE" 2>/dev/null)"
fi

if [ "$TEST_STATUS" -ne 0 ]; then
  refuse \
    "The unit-test suite failed (exit ${TEST_STATUS})." \
    "Command: $TEST_COMMAND" \
    "Elapsed: ${ELAPSED}s" \
    "" \
    "The task's acceptance criteria are not verified. Fix the failing tests or the" \
    "code under test; do not weaken, skip, or delete tests to pass this gate." \
    "" \
    "--- last ${MAX_STDERR_LINES} lines of test output ---" \
    "$(tail -n "$MAX_STDERR_LINES" "$LOG_FILE" 2>/dev/null)"
fi

echo "Gate satisfied: unit-test suite passed in ${ELAPSED}s (command: $TEST_COMMAND)."
echo "This gate can only refuse completion; it does NOT certify that the task's"
echo "acceptance criteria were met. Close the task only if they are satisfied."
exit 0
