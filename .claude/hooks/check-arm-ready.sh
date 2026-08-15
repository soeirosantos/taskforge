#!/usr/bin/env bash
#
# SessionStart hook: warns when a session is not properly set up for an
# experiment arm.
#
# run-arm.sh already refuses to launch a misconfigured arm, but it can be
# bypassed by running `claude` directly inside the container — which is exactly
# what happened on the first warmup run, and why an uncommitted gate config and
# a stale telemetry label both went unnoticed until after the session had done
# real work.
#
# This hook cannot block a session (SessionStart output is advisory), so it
# prints findings and always exits 0. It is a tripwire, not a gate.

set -uo pipefail

SCRIPT_DIR="$(cd "$(dirname "${BASH_SOURCE[0]}")" && pwd)"
PROJECT_DIR="${CLAUDE_PROJECT_DIR:-$(cd "$SCRIPT_DIR/../.." && pwd)}"
cd "$PROJECT_DIR" 2>/dev/null || exit 0

BRANCH="$(git rev-parse --abbrev-ref HEAD 2>/dev/null || echo unknown)"
[ "$BRANCH" = "main" ] && exit 0   # main is the control plane; nothing to check

WARNINGS=()

# Was this launched through run-arm.sh? That script exports the arm label.
case "${OTEL_RESOURCE_ATTRIBUTES:-}" in
  *experiment.arm=*)
    case "${OTEL_RESOURCE_ATTRIBUTES}" in
      *experiment.arm=unset*)
        WARNINGS+=("experiment.arm is 'unset' — telemetry cannot be attributed to this arm.") ;;
    esac ;;
  *)
    WARNINGS+=("OTEL_RESOURCE_ATTRIBUTES is not set. This session was probably not launched via .claude/sandbox/run-arm.sh, so its cost and token metrics cannot be attributed to an arm.") ;;
esac

# Is the gate configured, and is that configuration committed?
CONF="$SCRIPT_DIR/test-command.conf"
if [ -f "$CONF" ]; then
  TEST_COMMAND=""
  # shellcheck disable=SC1090
  . "$CONF" 2>/dev/null || true
  if [ -z "$(printf '%s' "$TEST_COMMAND" | tr -d ' \t\n')" ]; then
    WARNINGS+=("TEST_COMMAND is empty in .claude/hooks/test-command.conf — the gate will refuse every task completion on this branch.")
  elif ! git diff --quiet HEAD -- .claude/hooks/test-command.conf 2>/dev/null; then
    WARNINGS+=("test-command.conf is modified but uncommitted. A clean checkout of this branch would fail closed, so the arm is not reproducible.")
  fi
fi

# The gate script itself must match main, or arms are not comparable.
if ! git diff --quiet main..HEAD -- .claude/hooks/verify-unit-tests.sh 2>/dev/null; then
  WARNINGS+=("verify-unit-tests.sh differs from main. The experiment arms are running different apparatus.")
fi

[ ${#WARNINGS[@]} -eq 0 ] && exit 0

{
  echo "APPARATUS WARNINGS for branch '${BRANCH}':"
  echo
  for w in "${WARNINGS[@]}"; do echo "  - $w"; done
  echo
  echo "Fix these before treating this session's results as experiment data."
} >&2

exit 0
