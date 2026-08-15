#!/usr/bin/env bash
#
# Launches one experiment arm inside the sandbox container.
#
# Run from the HOST, on the branch you want to execute:
#
#     export CLAUDE_CODE_OAUTH_TOKEN=<token>   # from `claude setup-token`
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

# Auth. CLAUDE_CODE_OAUTH_TOKEN is the supported path for Claude subscribers
# running in non-interactive environments. Generate it on the host with
# `claude setup-token`.
#
# Only this method is supported for now. Other providers (Bedrock, Vertex,
# Console API keys) need their own env vars and are deliberately left out until
# there is a reason to add them.
[ -n "${CLAUDE_CODE_OAUTH_TOKEN:-}" ] || die \
  "CLAUDE_CODE_OAUTH_TOKEN is not set. Generate one on the host:

    claude setup-token                       # opens a browser
    export CLAUDE_CODE_OAUTH_TOKEN=<token>

  The token persists across runs, so this is a one-time step until it expires."

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
# Compared by content rather than by `git diff`: an arm's working tree is
# legitimately dirty for most of a run, and comparing committed state would
# either block real work or report drift that does not exist.
if [ "$BRANCH" != "main" ]; then
  MAIN_GATE="$(git show main:.claude/hooks/verify-unit-tests.sh 2>/dev/null || true)"
  THIS_GATE="$(cat .claude/hooks/verify-unit-tests.sh 2>/dev/null || true)"
  if [ -n "$MAIN_GATE" ] && [ "$MAIN_GATE" != "$THIS_GATE" ]; then
    die "verify-unit-tests.sh differs from main's version.
   The arms are running different apparatus and results are not comparable.
   Per-branch config belongs in $CONF, not in the gate script."
  fi
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

# The CLI version is apparatus too — a different agent build is a different
# experiment — so record it alongside the arm and the controls commit.
CLI_VERSION="$(grep -oE '^ARG CLAUDE_CODE_VERSION=.*' "$SCRIPT_DIR/Dockerfile" | cut -d= -f2)"
export OTEL_RESOURCE_ATTRIBUTES="experiment.arm=${ARM},apparatus.sha=${APPARATUS_SHA},cli.version=${CLI_VERSION:-unknown}"

# Build the image as a user matching yours, so files the agent creates in the
# mounted repo are owned by you and not by root. UID is readonly in bash, so it
# is passed to compose explicitly rather than exported.
COMPOSE_ENV=(env "UID=$(id -u)" "GID=$(id -g)")

COMPOSE_ENV+=("CLAUDE_CODE_OAUTH_TOKEN=${CLAUDE_CODE_OAUTH_TOKEN}")

# Compose substitutes ${OTEL_RESOURCE_ATTRIBUTES} from the environment of the
# `docker compose` process — which is this `env` prefix, not the parent shell.
# Exporting alone is not enough; the first run lost its arm label this way and
# produced telemetry that could not be attributed to an arm.
COMPOSE_ENV+=("OTEL_RESOURCE_ATTRIBUTES=${OTEL_RESOURCE_ATTRIBUTES}")

cat <<INFO
────────────────────────────────────────────────────────
 arm            : ${ARM}
 branch         : ${BRANCH}
 apparatus.sha  : ${APPARATUS_SHA}
 cli version    : ${CLI_VERSION:-unknown}
 test command   : ${TEST_COMMAND:-<unset — gate fails closed>}
 otlp endpoint  : http://otel-collector:4318
 mounted        : ${REPO_DIR} -> /work   (repo only)
────────────────────────────────────────────────────────
INFO

if [ $# -eq 0 ]; then
  # Named and NOT --rm, so the container survives a closed terminal. The first
  # run used `run --rm`: when the terminal died, Docker deleted the container
  # and its HOME, taking the task store with it and leaving nothing to
  # reconcile. Reattach after a disconnect with:
  #
  #     docker start -ai taskforge-arm-<arm>
  #
  CONTAINER="taskforge-arm-${ARM}"
  if docker ps -a --format '{{.Names}}' | grep -qx "$CONTAINER"; then
    echo "Reattaching to existing container '${CONTAINER}'."
    echo "To start fresh instead: docker rm -f ${CONTAINER}"
    echo
    exec docker start -ai "$CONTAINER"
  fi
  echo "Starting interactive shell in '${CONTAINER}'. Inside, run claude with:"
  echo "  claude --dangerously-skip-permissions"
  echo
  echo "If the terminal closes, the container keeps running. Reattach with:"
  echo "  docker start -ai ${CONTAINER}"
  echo
  exec "${COMPOSE_ENV[@]}" docker compose -f "$SCRIPT_DIR/docker-compose.yml" \
    run --name "$CONTAINER" sandbox bash
fi

# Headless: --rm is correct here, since the process owns the container's life.
exec "${COMPOSE_ENV[@]}" docker compose -f "$SCRIPT_DIR/docker-compose.yml" \
  run --rm sandbox claude --dangerously-skip-permissions "$@"
