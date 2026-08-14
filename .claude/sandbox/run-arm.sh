#!/usr/bin/env bash
#
# Launches one experiment arm inside the sandbox container.
#
# Run from the HOST, on the branch you want to execute:
#
#     export ANTHROPIC_API_KEY=sk-ant-...
#     .claude/sandbox/run-arm.sh                 # interactive shell in sandbox
#     .claude/sandbox/run-arm.sh -p "<spec>"     # headless run
#
# Its real job is to make the per-arm setup impossible to get wrong. It derives
# the arm name from the branch, stamps the apparatus commit, and refuses to run
# if the arm is misconfigured — because a run with the wrong labels produces
# data you cannot attribute, and you will not notice until afterward.

set -euo pipefail

SCRIPT_DIR="$(cd "$(dirname "${BASH_SOURCE[0]}")" && pwd)"
REPO_DIR="$(cd "$SCRIPT_DIR/../.." && pwd)"
cd "$REPO_DIR"

die() { echo "ERROR: $*" >&2; exit 1; }

[ -n "${ANTHROPIC_API_KEY:-}" ] || die \
  "ANTHROPIC_API_KEY is not set. Export it before running:
    export ANTHROPIC_API_KEY=sk-ant-..."

command -v docker >/dev/null || die "docker not found on PATH"
docker info >/dev/null 2>&1 || die "Docker is not running. Start Docker Desktop first."

# --- Derive arm identity from the branch -------------------------------------
BRANCH="$(git rev-parse --abbrev-ref HEAD)"
ARM="${BRANCH#experiment/}"
[ "$ARM" != "$BRANCH" ] || [ "$BRANCH" = "main" ] || die \
  "Branch '$BRANCH' is not an experiment branch.
   Expected experiment/<arm> (e.g. experiment/go), or main for a smoke test."

# The apparatus commit: the last commit on main that this branch descends from.
# Recording it is what lets you tell later whether two arms ran under identical
# controls.
APPARATUS_SHA="$(git merge-base HEAD main 2>/dev/null | cut -c1-7 || echo unknown)"

# --- Refuse to run a misconfigured arm ---------------------------------------
CONF=".claude/hooks/test-command.conf"
[ -f "$CONF" ] || die "Missing $CONF"
# shellcheck disable=SC1090
TEST_COMMAND=""; . "$CONF"
if [ "$BRANCH" != "main" ] && [ -z "$(printf '%s' "$TEST_COMMAND" | tr -d ' \t\n')" ]; then
  die "TEST_COMMAND is empty in $CONF on branch '$BRANCH'.
   The gate would refuse every task and the arm would produce nothing.
   Set it first, e.g. TEST_COMMAND=\"go test ./...\""
fi

# The gate script must be identical to main's, or the arms are not comparable.
if [ "$BRANCH" != "main" ] && ! git diff --quiet main..HEAD -- .claude/hooks/verify-unit-tests.sh 2>/dev/null; then
  die "verify-unit-tests.sh differs from main on this branch.
   The arms are running different apparatus and results are not comparable.
   Per-branch config belongs in $CONF, not in the gate script."
fi

# --- Warn (do not block) on a dirty tree -------------------------------------
if [ -n "$(git status --porcelain)" ]; then
  echo "WARNING: working tree is dirty. The run will include uncommitted changes." >&2
  echo "         Commit first if you want the arm to be reproducible." >&2
  echo >&2
fi

# --- Metrics stack -----------------------------------------------------------
if ! docker network inspect metrics_default >/dev/null 2>&1; then
  die "Network 'metrics_default' not found. Start the metrics stack first:
    (cd .claude/metrics && docker compose up -d)"
fi
if ! curl -s --max-time 2 http://localhost:9090/-/healthy >/dev/null 2>&1; then
  echo "WARNING: Prometheus is not responding on :9090. Metrics may not be recorded." >&2
  echo >&2
fi

export OTEL_RESOURCE_ATTRIBUTES="experiment.arm=${ARM},apparatus.sha=${APPARATUS_SHA}"

# Build the image as a user matching yours, so files the agent creates in the
# mounted repo are owned by you and not by root. UID is readonly in bash, so it
# is passed to compose explicitly rather than exported.
COMPOSE_ENV=(env "UID=$(id -u)" "GID=$(id -g)")

cat <<INFO
────────────────────────────────────────────────────────
 arm            : ${ARM}
 branch         : ${BRANCH}
 apparatus.sha  : ${APPARATUS_SHA}
 test command   : ${TEST_COMMAND:-<unset — gate fails closed>}
 otlp endpoint  : http://otel-collector:4318
 mounted        : ${REPO_DIR} -> /work   (repo only)
────────────────────────────────────────────────────────
INFO

if [ $# -eq 0 ]; then
  echo "Starting interactive shell. Inside, run claude with:"
  echo "  claude --dangerously-skip-permissions"
  echo
  exec "${COMPOSE_ENV[@]}" docker compose -f "$SCRIPT_DIR/docker-compose.yml" \
    run --rm sandbox bash
fi

# Headless: pass everything through to claude inside the container.
exec "${COMPOSE_ENV[@]}" docker compose -f "$SCRIPT_DIR/docker-compose.yml" \
  run --rm sandbox claude --dangerously-skip-permissions "$@"
