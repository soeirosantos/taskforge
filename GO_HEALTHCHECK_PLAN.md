# Task Plan — Go Health Check Service

Planning output for `GO_HEALTHCHECK_SPEC.md`, produced under
`.claude/TASK_PLANNING_INSTRUCTIONS.md` and the bounded-execution policy in
`CLAUDE.md`. **No implementation has been performed.** Tasks below will be
created with `TaskCreate` at the start of the execution phase, after review.

---

## 1. Specification analysis

### Requested behavior

A minimal Go HTTP service exposing `GET /health`, runnable with `go run .`.

### Functional requirements

| # | Requirement |
|---|---|
| F1 | `GET /health` → `200`, `Content-Type: application/json`, body `{"status":"ok"}` |
| F2 | `/health` with any other method → `405 Method Not Allowed` |
| F3 | Undefined routes (e.g. `GET /unknown`) → `404 Not Found` |
| F4 | Listen on port `8080` by default; `PORT` env var overrides it |
| F5 | Log at startup that the server is starting, including the configured port (format free) |
| F6 | Automated tests covering the five cases in spec §7, runnable via `go test ./...`, requiring no manually started server |
| F7 | Root `README.md` covering purpose, run, test, verification, and how to call the endpoint |

### Non-functional requirements and constraints

- Go, standard library only where practical; no third-party frameworks, routers,
  DI, config libs, persistence, Docker, middleware, metrics, or auth.
- Simplest reasonable project structure.
- `go build ./...`, `go test ./...`, `go vet ./...` must succeed; sources must
  already be gofmt-clean (no post-hoc formatting to satisfy the gate).
- No verification step may be skipped, weakened, or suppressed.

### Invariants

- Exactly one endpoint. No readiness/liveness endpoints beyond `/health`.
- No graceful-shutdown logic, TLS, tracing, or business logic.
- Everything under `.claude/` is harness and must not be modified by any
  implementation task (`CLAUDE.md`). The application owns the repository root.

### Out of scope

Per spec §12: database access, external services, authn/authz, TLS, Docker,
Kubernetes, Prometheus metrics, tracing, graceful shutdown, extra endpoints,
business logic.

### Success criteria

Spec §11 "Definition of Done" — all nine checks.

---

## 2. Repository findings that shape the plan

Inspected: `git ls-files`, `.claude/settings.json`,
`.claude/hooks/verify-unit-tests.sh`, `.claude/hooks/test-command.conf`, the
three agent profiles, `.gitignore`, and the Go toolchain.

1. **There is no application code.** Tracked files are the harness under
   `.claude/`, `CLAUDE.md`, `AGENT_CONTROL_SETUP.md`, and `.gitignore`. There is
   no `go.mod`, no existing Go source, no prior test organization, and no
   language convention to extend. This is a greenfield application inside a
   pre-existing harness, so "prefer extending existing patterns" applies only to
   the harness, which implementation tasks must not touch.
2. **Toolchain:** `go1.23.5 linux/arm64` at `/usr/local/go/bin/go`,
   `GOFLAGS=-mod=mod`, `GOTOOLCHAIN=auto`. Go ≥ 1.22 means
   `http.ServeMux` supports method-and-path patterns (`"GET /health"`), which
   yields `405` for other methods and `404` for unknown paths from the standard
   library alone. No third-party router is needed or permitted.
3. **The completion gate runs `go test ./...`** with a 300 s timeout
   (`.claude/hooks/test-command.conf`), enforced by the blocking `TaskCompleted`
   hook. Measured behavior of that exact command in this environment:

   | Repository state | `go test ./...` exit | Gate outcome |
   |---|---|---|
   | No `go.mod` | 1 (`directory prefix . does not contain main module`) | **refuses** |
   | `go.mod`, no `.go` files | 1 (`matched no packages`) | **refuses** |
   | `go.mod` + compiling package, no `_test.go` | 0 (`[no test files]`) | passes |

   Two consequences drive the decomposition below:
   - **A `go.mod`-only scaffolding task cannot be closed** — the gate refuses it.
     Module initialization must therefore be bundled with the first task that
     produces a compiling package.
   - **The gate passes vacuously for Task 1** (a package with no test files
     exits 0). Task 1's real evidence must come from its task-specific
     verification, not the gate. This is exactly the "necessary, not sufficient"
     property the harness documents.
4. **Formatting check:** `gofmt -l .` prints nothing when sources are clean;
   the spec's requirement that sources are *already* formatted maps to
   `test -z "$(gofmt -l .)"`.
5. **Module fetching is unnecessary** (stdlib only). The proxy happens to be
   reachable, but no task should add a `require` directive. `GOTOOLCHAIN=auto`
   means the `go` directive in `go.mod` must not exceed `1.23.5`; `go mod init`
   picks the local version, so leave it alone.
6. **Escalation records** go to `.claude/escalations/<task-id>.md` from
   `TEMPLATE.md`. Written by the orchestrator only — not by workers, and not as
   an edit to harness behavior.

### Structural decision (documented, not invented)

Single `package main` at the repository root: `go.mod`, `main.go`,
`main_test.go`, `README.md`. This is the smallest structure satisfying
`go run .` and `go build ./...`, and it avoids the `cmd/` + `internal/` layout
the spec's "simplest reasonable project structure" rules out.

One design constraint follows from F6 combined with F2/F3: **`404` and `405` are
produced by the router, not by the handler**, so a test cannot exercise them
through a bare `http.HandlerFunc`. The implementation must expose the configured
`*http.ServeMux` (or an `http.Handler`) from a function in `package main` that
tests can call, e.g. `func newMux() *http.ServeMux`. Tests then drive it with
`net/http/httptest` and never bind a port. This is the only structural
requirement the plan imposes on Task 1.

---

## 3. Tasks

### Task 1: HTTP service with health endpoint and routing behavior

**Objective**

A compiling, runnable Go module at the repository root that serves
`GET /health` with the specified status, content type, and body; returns `405`
for other methods on `/health` and `404` for unknown routes; listens on `8080`
or `$PORT`; and logs the configured port at startup.

**Scope**

New files at the repository root: `go.mod`, `main.go`. No files under
`.claude/`. No dependencies beyond the standard library.

**Dependencies**

`None`

**Parallelizable**

`No` — every other task depends on this one.

**Complexity** `low`  **Ambiguity** `low`  **Risk** `medium`

**Initial execution profile**

`worker-sonnet`

**Profile rationale**

Application implementation, which the policy assigns to `worker-sonnet` by
default. It is small, but it is not mechanical: routing semantics, the
port-override path, and the testability seam (below) require engineering
judgment. Risk is `medium` rather than `low` because this task defines the
package structure and the exported-to-tests seam that Tasks 2–4 depend on, and
because its completion gate passes vacuously (no test files yet), so a defect
here surfaces only downstream.

**Implementation guidance**

- `package main` at the repository root. Module path `healthcheck` (no VCS path
  is required; nothing imports this module).
- Do not add any `require` directive. Standard library only: `net/http`,
  `encoding/json` or a literal JSON write, `log`, `os`.
- **Factor routing behind a function such as `func newMux() *http.ServeMux`**
  that builds and returns the fully configured handler, and have `main` use it.
  Task 2 must be able to obtain the routed handler without starting a listener;
  `404` and `405` are router behavior and are untestable otherwise.
- Go 1.22+ `http.ServeMux` method patterns (`mux.HandleFunc("GET /health", …)`)
  give `405` on other methods and `404` on unknown paths from the stdlib. An
  explicit method check is acceptable if it produces the same observable
  behavior; do not add a third-party router either way.
- Set `Content-Type: application/json` explicitly before writing the body.
  Writing the body first causes Go to sniff and set `text/plain`.
- Port resolution: `PORT` if non-empty, else `8080`. Keep it in a small helper so
  the default is stated in exactly one place.
- Log the startup line before `ListenAndServe` blocks, including the port.
- No graceful shutdown, no extra endpoints, no middleware, no metrics (spec §12).
- Leave sources gofmt-clean as written.

**Acceptance criteria**

- The module builds: `go build ./...` succeeds from a clean checkout.
- Running the service and requesting `GET /health` returns `200`, a
  `Content-Type` of `application/json`, and a body that parses as JSON with
  `status` equal to `ok`.
- `POST /health` against the running service returns `405`.
- `GET /unknown` against the running service returns `404`.
- Starting with no `PORT` set listens on `8080`; starting with `PORT=9090`
  listens on `9090`.
- Startup output names the port the server is listening on.
- The routed handler is reachable from `package main` test code without binding
  a network port.
- Only `go.mod` and `main.go` are added; nothing under `.claude/` is modified.

**Task-specific verification**

```bash
go build ./...
go vet ./...
test -z "$(gofmt -l .)"   # no output = formatted
# behavioral probe, torn down afterwards:
go run . & sleep 1
curl -si  http://localhost:8080/health          # expect 200 + application/json + {"status":"ok"}
curl -s -o /dev/null -w '%{http_code}\n' -X POST http://localhost:8080/health   # expect 405
curl -s -o /dev/null -w '%{http_code}\n'      http://localhost:8080/unknown     # expect 404
kill %1
PORT=9090 go run . & sleep 1
curl -s -o /dev/null -w '%{http_code}\n' http://localhost:9090/health           # expect 200
kill %1
```

**Completion condition**

The acceptance criteria are demonstrated by the actual output of the commands
above — pasted, not paraphrased — and the `TaskCompleted` gate passes. Note
explicitly that the gate is weak here: with no `_test.go` files,
`go test ./...` exits 0 on any compiling package. The behavioral probe output,
not the gate, is the evidence for this task.

**Escalation**

```text
worker-sonnet
    ↓ bounded attempt unsuccessful
escalation-opus
    ↓ bounded attempt unsuccessful
STOP / human escalation
```

Downstream of this task: **Tasks 2, 3, 4** — all halt if this task stops.

---

### Task 2: Automated tests for the health endpoint and routing

**Objective**

A Go test suite that verifies the five behaviors required by spec §7 and runs
green under `go test ./...` without any manually started server.

**Scope**

New file at the repository root: `main_test.go`, `package main`. No changes to
`main.go` except as needed to fix a defect this task uncovers (report it if so).
No files under `.claude/`.

**Dependencies**

`Task 1`

**Parallelizable**

`Yes` — safe to run alongside Task 3, which touches only `README.md`.

**Complexity** `low`  **Ambiguity** `low`  **Risk** `medium`

**Initial execution profile**

`worker-sonnet`

**Profile rationale**

Unit-test implementation is explicitly `worker-sonnet` work. Risk is `medium`
because this suite becomes the repository completion gate for every subsequent
task: a test that asserts the wrong thing, or that passes vacuously, silently
weakens the gate for the rest of the plan.

**Implementation guidance**

- Use `net/http/httptest` (`httptest.NewRecorder` + `httptest.NewRequest`)
  against the handler returned by Task 1's `newMux()`. Do not call
  `ListenAndServe`, do not bind a port, do not use `httptest.NewServer` unless
  a recorder genuinely cannot express the assertion.
- Required cases (spec §7), one assertion set each:
  1. `GET /health` → status `200`;
  2. response `Content-Type` is `application/json` (compare on the media type;
     a `; charset=utf-8` suffix should not fail the test if the implementation
     emits one);
  3. body unmarshals into a struct or `map[string]string` and `status == "ok"`
     — assert on the decoded value, not on a raw byte-for-byte string, so
     whitespace and key order do not make the test brittle;
  4. `POST /health` → status `405`;
  5. `GET /unknown` → status `404`.
- Table-driven subtests are fine; so are five plain test functions. Do not add
  helpers, fixtures, or a test framework dependency.
- Do not test behavior the spec does not require, and do not weaken an assertion
  to make it pass — if the implementation is wrong, fix `main.go` and say so.
- Leave sources gofmt-clean.

**Acceptance criteria**

- `go test ./...` passes and reports the health package as tested (not
  `[no test files]`).
- All five spec §7 behaviors are covered by distinct, named assertions.
- The suite passes with no server started by hand and binds no listening port.
- Each test fails when the corresponding behavior is broken — verified by
  temporarily perturbing the implementation and observing the specific test
  fail, then reverting. The revert must leave `main.go` byte-identical.
- No `require` directive is added to `go.mod`.

**Task-specific verification**

```bash
go test ./...          # expect ok, not "[no test files]"
go test -v -run . ./... # named test output for each of the five behaviors
go vet ./...
test -z "$(gofmt -l .)"
git diff --stat        # after the perturb/revert check: main.go must show no change
```

**Completion condition**

Real `go test -v` output showing the five behaviors passing, plus the
perturb-and-revert evidence that the assertions actually bite, plus a clean
`git diff` for `main.go`. Then the `TaskCompleted` gate. From this task onward
the gate is meaningful rather than vacuous.

**Escalation**

```text
worker-sonnet
    ↓ bounded attempt unsuccessful
escalation-opus
    ↓ bounded attempt unsuccessful
STOP / human escalation
```

Downstream of this task: **Task 4**.

---

### Task 3: Root README documenting execution and verification

**Objective**

A short root `README.md` covering the application's purpose, how to run it, how
to run tests, how to run the verification commands, and how to call the health
endpoint.

**Scope**

New file `README.md` at the repository root. `CLAUDE.md` states there is no
name collision with harness documentation
(`.claude/AGENT_CONTROL_README.md`) and no reason to rename it. No other files.

**Dependencies**

`Task 1` — the documented commands must match what actually exists.

**Parallelizable**

`Yes` — safe alongside Task 2; disjoint files.

**Complexity** `low`  **Ambiguity** `low`  **Risk** `low`

**Initial execution profile**

`worker-haiku`

**Profile rationale**

Straightforward, isolated documentation of commands that are already fixed by
the specification and by Task 1's output. No engineering judgment or
cross-component reasoning is required, which is squarely `worker-haiku` scope.

**Implementation guidance**

- Keep it short. Cover exactly the five items in spec §10.
- Required content: purpose; `go run .`; `go test ./...`; the verification set
  (`go build ./...`, `go test ./...`, `go vet ./...`, `gofmt -l .`); and the
  example request `curl http://localhost:8080/health` with its expected
  `{"status":"ok"}` response.
- Document the `PORT` override and the `8080` default, since that is service
  behavior a reader needs in order to run it.
- Every command must be copy-pasteable and actually work in this repository —
  run them before writing them down.
- Do not document the `.claude/` harness, the agent profiles, or the experiment.
  This README is the application's.

**Acceptance criteria**

- `README.md` exists at the repository root and covers all five spec §10 items.
- Every command shown runs successfully as written from the repository root.
- The documented health request and response match the service's actual output.
- The `PORT` override and `8080` default are stated.
- No harness or experiment content appears in it.

**Task-specific verification**

```bash
test -f README.md
# execute each documented command verbatim from the repo root and record output:
go build ./... && go test ./... && go vet ./... && gofmt -l .
go run . & sleep 1; curl -s http://localhost:8080/health; kill %1
```

**Completion condition**

Actual output of every documented command, showing it succeeds as written, plus
the `TaskCompleted` gate.

**Escalation**

```text
worker-haiku
    ↓ bounded attempt unsuccessful
escalation-opus
    ↓ bounded attempt unsuccessful
STOP / human escalation
```

Downstream of this task: **Task 4**.

---

### Task 4: Definition-of-Done verification sweep

**Objective**

Deterministic evidence that the integrated repository satisfies every one of the
nine checks in spec §11, gathered from a clean state after all prior tasks.

**Scope**

Read-and-run only. This task produces evidence, not code. If any check fails, it
reports the failure and does **not** repair it — the failure is routed back to
the owning task by the orchestrator.

**Dependencies**

`Task 1`, `Task 2`, `Task 3`

**Parallelizable**

`No` — it is the integration checkpoint.

**Complexity** `low`  **Ambiguity** `low`  **Risk** `low`

**Initial execution profile**

`worker-haiku`

**Profile rationale**

Command execution and deterministic evidence gathering, with no code changes and
no interpretation beyond comparing observed output against a fixed checklist —
explicitly `worker-haiku` scope. It is not a "run the tests" filler task: it is
the only step that exercises the assembled service end to end over real HTTP,
which the unit suite deliberately does not do.

**Implementation guidance**

- Run each check from the repository root and capture verbatim output.
- Do not modify any file. Do not fix anything found. Report and stop.
- Confirm `git status` shows only the expected application files
  (`go.mod`, `main.go`, `main_test.go`, `README.md`, plus the spec and plan
  markdown) and **no** modifications under `.claude/`.
- Tear down any server started for the HTTP probes.

**Acceptance criteria**

Each of the nine spec §11 conditions is backed by captured output:

| Check | Evidence |
|---|---|
| `go build ./...` succeeds | exit 0 |
| `go test ./...` succeeds | exit 0, package reported as tested |
| `go vet ./...` succeeds | exit 0, no diagnostics |
| gofmt-compliant | `gofmt -l .` prints nothing |
| `GET /health` → 200 | HTTP status line |
| `GET /health` → `{"status":"ok"}` | response body + `application/json` header |
| `POST /health` → 405 | HTTP status |
| unknown route → 404 | HTTP status |
| README documents execution and verification | each documented command re-run successfully |

Plus: `git diff --stat main..HEAD -- .claude/` is empty apart from
`test-command.conf`, confirming the apparatus was not altered.

**Task-specific verification**

```bash
go build ./... ; echo "build=$?"
go test ./...  ; echo "test=$?"
go vet ./...   ; echo "vet=$?"
gofmt -l .     ; echo "gofmt-listed-files-above (empty = compliant)"
go run . & sleep 1
curl -si  http://localhost:8080/health
curl -s -o /dev/null -w 'POST /health -> %{http_code}\n' -X POST http://localhost:8080/health
curl -s -o /dev/null -w 'GET /unknown -> %{http_code}\n'      http://localhost:8080/unknown
kill %1
git status --short
git diff --stat main..HEAD -- .claude/
```

**Completion condition**

A report mapping all nine Definition-of-Done items to captured output, with no
item unverified, plus the `TaskCompleted` gate. Any failing item makes this task
`RESULT: failure`; the orchestrator then reopens the owning task rather than
letting this task patch it.

**Escalation**

```text
worker-haiku
    ↓ bounded attempt unsuccessful
escalation-opus
    ↓ bounded attempt unsuccessful
STOP / human escalation
```

Downstream of this task: none. It is terminal.

---

## 4. Execution graph and waves

```text
Task 1 (module + service)
   │
   ├── Task 2 (tests) ──┐
   │                    ├── Task 4 (DoD verification sweep)
   └── Task 3 (README) ─┘
```

```text
Wave 1: Task 1

Wave 2:
  Task 2   (main_test.go)
  Task 3   (README.md)

Wave 3: Task 4
```

Wave 2 is genuinely parallel: the two tasks write disjoint files, share no
interface, and neither reads the other's output. Wave 1 and Wave 3 are
serialized by real dependencies, not by caution.

---

## 5. Risk and ambiguity summary

### Highest-risk tasks

- **Task 1** — highest risk in the plan despite low complexity, because its
  completion gate is *vacuous*: measured above, `go test ./...` exits 0 for a
  compiling package with no test files, so the hook cannot detect a wrong
  status code, a missing `Content-Type`, or a broken `PORT` override. Mitigation:
  the behavioral `curl` probe in its task-specific verification is mandatory
  evidence, and Task 2 re-checks the same behaviors under the real gate.
- **Task 2** — this suite *becomes* the repository gate. A vacuous or
  wrongly-scoped assertion weakens every downstream completion check.
  Mitigation: the perturb-and-revert requirement forces each test to demonstrate
  that it fails when its behavior breaks, with `git diff` proving the revert was
  clean.

Tasks 3 and 4 are low risk: one writes prose whose commands are executed as
verification, the other changes nothing.

### Material assumptions

1. `go1.23.5` remains the toolchain, so `http.ServeMux` method patterns are
   available and `go.mod`'s `go` directive stays at or below the installed
   version (`GOTOOLCHAIN=auto` would otherwise try to fetch a toolchain).
2. Single root `package main` is the "simplest reasonable project structure"
   intended by spec §9. Recorded as an assumption because the spec does not name
   a layout.
3. Module path `healthcheck`. Nothing imports this module, so the path is
   inconsequential; a VCS-style path would be equally valid.
4. Binding `localhost:8080` and `localhost:9090` during verification is possible
   in the execution environment. If a port is occupied, verification uses
   `PORT` to pick another — this changes the probe, not the requirement.
5. `Content-Type: application/json; charset=utf-8` satisfies F1. The spec writes
   the header without a charset; the assertion compares media type so either
   form passes.
6. Spec §7's five cases are the *complete* required test set ("at minimum" is
   read together with §12's instruction to add nothing further).

### Unresolved ambiguities

None that prevent implementation. The following are handled by documented
assumption:

- **Which non-GET methods must return 405.** The spec names `POST`. Stdlib
  method routing returns 405 for every method except `GET`/`HEAD`; that is a
  superset of the requirement and is accepted.
- **Whether `HEAD /health` should succeed.** Unspecified. Go's `ServeMux`
  matches `HEAD` to a `GET` pattern; left as the stdlib default rather than
  special-cased.
- **Startup log destination and format.** Spec §6 calls the format
  implementation-specific; `log` to stderr is the stdlib default and is accepted.
- **Exact JSON whitespace.** Unspecified; asserted on decoded values, not bytes.

### Cross-task risks

- **Shared seam.** Tasks 1 and 2 both depend on the `newMux()`-style accessor.
  If Task 1 does not provide it, Task 2 cannot test `404`/`405` without binding
  a port. This is called out as a Task 1 acceptance criterion specifically to
  keep it from becoming a Wave 2 rework.
- **Same-package file conflict.** `main.go` and `main_test.go` are in one
  package. Task 2 may need a small fix in `main.go`; since Task 3 does not touch
  Go sources, Wave 2 still has no write conflict.
- **Gate coupling.** Every task after Task 2 is gated by Task 2's suite. A
  flaky or over-broad test would block unrelated completions; the "no helpers,
  no fixtures, no ports" guidance keeps the suite deterministic.
- **Harness immutability.** Every task is forbidden from touching `.claude/`.
  Task 4 verifies this with `git diff main..HEAD -- .claude/`, which must show
  only `test-command.conf`. A worker "fixing" the gate is an experiment-level
  failure, not a repair.
- **Ordering.** Task 3's documented commands must reflect the final state; if
  Task 2 changes `main.go`, the README's claims are re-verified by Task 4 rather
  than assumed still true.

---

## 6. Resource allocation summary

| Task | Initial profile | Complexity | Ambiguity | Risk | Dependencies |
|---|---|---|---|---|---|
| Task 1 — module + HTTP service | worker-sonnet | low | low | medium | None |
| Task 2 — automated tests | worker-sonnet | low | low | medium | Task 1 |
| Task 3 — root README | worker-haiku | low | low | low | Task 1 |
| Task 4 — DoD verification sweep | worker-haiku | low | low | low | Tasks 1, 2, 3 |

**No task is assigned directly to `escalation-opus`.** Nothing in this
specification involves substantial architectural reasoning, high ambiguity,
cross-cutting change, concurrency, or security-sensitive behavior. Opus remains
available strictly as the single bounded escalation attempt after a normal
worker's attempt fails.

Per the policy, when escalating the orchestrator must hand `escalation-opus` the
prior worker's approach, why it failed, and the verbatim deterministic
verification output — Opus starts with no memory of the failed attempt.

---

## 7. Overall verification strategy

### Task-level verification

Fast, targeted checks workers run while implementing: `go build ./...`,
`go vet ./...`, `gofmt -l .`, `go test -run <name> ./...` for the specific
behavior under construction, and `curl` probes against a locally started
instance for the behaviors the unit suite does not cover.

### Repository completion gate

The blocking `TaskCompleted` hook `.claude/hooks/verify-unit-tests.sh` runs
`TEST_COMMAND="go test ./..."` with a 300 s timeout. It fails closed. It can
only refuse completion; a pass means nothing blocking was detected, not that the
acceptance criteria were met. Its weakness in Wave 1 (a package with no tests
exits 0) is compensated by Task 1's behavioral probe and by Task 2 landing the
real suite immediately after.

Nothing in this plan modifies the gate, its configuration, or its registration.

### Specification-level verification

Task 4 maps all nine spec §11 Definition-of-Done conditions to captured command
output, including the HTTP behaviors exercised over a real socket and a
confirmation that `.claude/` is unmodified. No new coverage, performance,
security, or integration thresholds are introduced — the spec asks for none.

---

## 8. Planning phase status

Plan complete and **approved**. No application code has been written and no
tasks have been created or closed.

The planning session could not enter the execution phase: it had no `TaskCreate`
tool, so no task could run the `TaskCompleted` gate. Cause was session lifetime,
not configuration — Claude Code 2.1.233 enables the task tools by default from a
boolean feature flag, and a fresh `claude -p` session on the same build reported
`TaskCreate, TaskGet, TaskList, TaskOutput, TaskStop, TaskUpdate`. A session's
tool list is fixed at startup, so only a restart changes it.

Two apparatus corrections were made before handoff:

- `CLAUDE_CODE_ENABLE_TASKS: "1"` was removed from `.claude/settings.json`. It
  is inert on 2.1.233 and was deliberately dropped on `main` in `8541043`.
  `git diff main -- .claude/` is now exactly the one `test-command.conf` line,
  as the apparatus invariant requires.
- The planning session was launched with plain `claude`, so
  `OTEL_RESOURCE_ATTRIBUTES` was unset and its telemetry carries no
  `experiment.arm` label. Execution must be launched through
  `.claude/sandbox/run-arm.sh`.

`CLAUDE.md`'s description of `CLAUDE_CODE_ENABLE_TASKS` is inaccurate for this
CLI version; that is a `main` bug being fixed there and was deliberately left
untouched on this branch.

**Next step for the execution session:** create these four tasks with
`TaskCreate` and dispatch Wave 1 (Task 1).
