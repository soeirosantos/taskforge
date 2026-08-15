# Task Plan: Go Health Check Service

## 1. Specification summary

**In scope:** single-binary Go HTTP service; `GET /health` → `200`, `Content-Type: application/json`, body `{"status":"ok"}`; non-GET on `/health` → `405`; unknown routes → `404`; startup log naming the port; `PORT` env override (default `8080`); stdlib-only; tests runnable via `go test ./...` without manually starting a server; passes `go build ./...`, `go vet ./...`, `gofmt` (already-formatted, not reformatted-on-demand); root-level `README.md`.

**Explicitly out of scope:** auth, TLS, Docker, persistence, metrics, tracing, graceful shutdown, extra endpoints, any framework/router/DI/config library.

**Invariant:** simplest possible structure — single `package main` at repo root, stdlib only.

## 2. Repository inspection findings

- Root is currently empty of application code; `.claude/` holds only harness.
- `.claude/hooks/test-command.conf` (uncommitted on this branch) already sets `TEST_COMMAND="go test ./..."`, `TEST_TIMEOUT_SECONDS=300`. This is harness state, correctly set for this experiment arm — no task should touch it.
- `verify-unit-tests.sh` only runs `TEST_COMMAND` (i.e., `go test ./...`). It does **not** run `go vet` or `gofmt`. Those are required by SPEC §8/§11 but are **not** enforced by the blocking `TaskCompleted` gate — they must be verified as task-specific/spec-level checks by the orchestrator, not assumed to be covered by the hook.
- Go toolchain: `go1.23.5 linux/arm64`.
- No existing Go conventions to extend (greenfield); default to idiomatic stdlib `net/http` patterns.

## 3. Task decomposition

### Task 1: Implement the Go HTTP health-check service

**Objective**
A runnable Go service satisfying all functional requirements: `GET /health` returns the specified 200/JSON response, non-GET `/health` returns 405, unknown routes return 404, port is `8080` by default and overridable via `PORT`, and a startup log line names the configured port.

**Scope**
- New `go.mod` at repo root (module path is an internal detail; module directive `go 1.23`).
- New `main.go` at repo root, `package main`, using only `net/http`, `encoding/json`, `log`, `os`.
- The `/health` handling logic should be a package-level function (e.g. an `http.HandlerFunc`) that Task 2's tests can invoke directly without starting a real listener.

**Dependencies**
`None`

**Parallelizable**
`Yes` — can run alongside Task 3 (README). Different files, no shared interface Task 3 needs beyond what SPEC.md already fixes (commands, port, endpoint path).

**Complexity** `low` · **Ambiguity** `low` · **Risk** `low`

**Initial execution profile**
`worker-sonnet`

**Profile rationale**
This is genuine feature implementation (routing/method dispatch without a router library, JSON encoding, env parsing) rather than a purely mechanical edit, so it falls under worker-sonnet's default scope rather than worker-haiku's. Risk is low (no auth, no data, no external interfaces), so escalation to Opus at the outset is not warranted.

**Implementation guidance**
- Keep everything in one `main.go` at repo root — no `cmd/`, `internal/`, or `pkg/` subdirectories; no router library.
- Distinguish "unknown route" (404) from "known route, wrong method" (405) — with stdlib `http.ServeMux` alone this requires explicit method checking inside the `/health` handler (or manual path/method dispatch), since a bare mux only 404s on unmatched paths.
- Read `PORT` from `os.Getenv("PORT")`; default to `"8080"` when unset or empty.
- Log a startup line that includes the resolved port before calling `ListenAndServe`.
- No graceful shutdown, no middleware, no additional endpoints — SPEC §9/§12 explicitly forbid them.
- Must build cleanly with `go build ./...` and be runnable via `go run .`.

**Acceptance criteria**
- `go run .` starts a server; `curl http://localhost:8080/health` returns `200`, `Content-Type: application/json`, body `{"status":"ok"}`.
- Setting `PORT=9090` and running causes the service to listen on `9090` instead.
- `POST http://localhost:8080/health` returns `405`.
- `GET http://localhost:8080/unknown` returns `404`.
- Startup output names the port being used.
- `go build ./...` succeeds with no errors.

**Task-specific verification**
- `go build ./...`
- `go vet ./...`
- `gofmt -l .` (must produce empty output — no unformatted files)
- Manual smoke check: run the binary, `curl` `/health` with GET and POST, and an unknown path, confirming status codes and body.

**Completion condition**
Acceptance criteria demonstrably satisfied (build succeeds, manual curl checks match spec) **and**, once Task 2 lands, the repository `TaskCompleted` gate (`go test ./...`) passes.

**Escalation**
```
worker-sonnet → bounded attempt fails → escalation-opus → bounded attempt fails → STOP / human
```

---

### Task 2: Automated tests for the health endpoint

**Objective**
A test suite that deterministically verifies all 5 behaviors SPEC §7 requires, runnable via `go test ./...` with no manual server startup.

**Scope**
- New `main_test.go` (or `health_test.go`), `package main`, same directory as `main.go`.
- Use `net/http/httptest` (`httptest.NewRecorder` + direct handler invocation, or `httptest.NewServer` wrapping the handler) — no real network listener/manual start required.

**Dependencies**
`Task 1` (needs the handler function/signature to exist and be exported at package level to test against)

**Parallelizable**
`No` — must wait for Task 1's handler to exist.

**Complexity** `low` · **Ambiguity** `low` · **Risk** `low`

**Initial execution profile**
`worker-sonnet`

**Profile rationale**
Test-writing is explicit worker-sonnet scope. While mechanically simple, correctly asserting status code + `Content-Type` + JSON body shape across 5 distinct cases benefits from the same reasoning tier as the implementation, not the minimal-reasoning haiku tier.

**Implementation guidance**
- Cover exactly the 5 cases in SPEC §7: `GET /health` → 200; response `Content-Type: application/json`; JSON body decodes to `{"status":"ok"}`; `POST /health` → 405; `GET /unknown` (or similar) → 404.
- Test against the handler directly (`httptest.NewRecorder`) so `go test ./...` never binds a real port.
- Do not weaken, skip, or delete any part of the required coverage to make tests pass.

**Acceptance criteria**
- `go test ./...` exits 0 and its output shows assertions for all 5 required behaviors.
- Deliberately breaking the handler would make the relevant test fail — i.e., tests are not vacuous.

**Task-specific verification**
- `go test ./... -v`
- `go vet ./...`
- `gofmt -l .` (empty output)

**Completion condition**
Acceptance criteria satisfied **and** the repository `TaskCompleted` gate (`go test ./...`) passes, with non-trivial `PASS`/`ok` output confirming real test execution.

**Escalation**
```
worker-sonnet → bounded attempt fails → escalation-opus → bounded attempt fails → STOP / human
```
Downstream: none.

---

### Task 3: README.md

**Objective**
A root-level `README.md` documenting purpose, how to run, how to test, how to verify, and how to call the health endpoint, per SPEC §10.

**Scope**
- New `README.md` at repo root only.

**Dependencies**
`None`

**Parallelizable**
`Yes` — Wave 1, alongside Task 1.

**Complexity** `low` · **Ambiguity** `low` · **Risk** `low`

**Initial execution profile**
`worker-haiku`

**Profile rationale**
Pure documentation task with content requirements spelled out verbatim in SPEC §10 — mechanical summarization, no design decisions.

**Implementation guidance**
- Include: purpose, run instructions (`go run .`, `PORT` override), test instructions (`go test ./...`), verification instructions (`go vet ./...`, `gofmt`, `go build ./...`), and an example `curl http://localhost:8080/health` call with expected response.
- Keep it short.

**Acceptance criteria**
- `README.md` exists at repo root and documents all five items SPEC §10 requires.
- Every command shown matches SPEC.md / Task 1's actual implementation.

**Task-specific verification**
- Manual review: diff README commands against SPEC §2/§7/§8 and Task 1's actual `PORT` handling for consistency.

**Completion condition**
Acceptance criteria satisfied by inspection.

**Escalation**
```
worker-haiku → bounded attempt fails → escalation-opus → bounded attempt fails → STOP / human
```
Downstream: none.

## 4. Execution graph

```text
Task 1 (service impl) ──────┐
                             ├──> Task 2 (tests)
Task 3 (README) ─────────────    [independent, no downstream]
```

```text
Wave 1:
  Task 1  (worker-sonnet)
  Task 3  (worker-haiku)

Wave 2:
  Task 2  (worker-sonnet)   [depends on Task 1]
```

## 5. Risk and ambiguity summary

**Highest-risk task:** None carry meaningful risk. Task 1 is closest to "highest risk" only because Task 2 depends on it, mitigated by manual curl checks in Task 1's own verification before Task 2 lands.

**Material assumptions:**
- Go module path is an internal, non-functional detail; any reasonable name is fine.
- Single `main.go` at repo root (no subpackages) satisfies "simplest reasonable project structure".
- `go.mod`'s `go` directive tracks the installed toolchain (`go 1.23`, confirmed via `go version` = 1.23.5).

**Unresolved ambiguities:** None blocking. SPEC.md is precise on every required behavior.

**Cross-task risks:**
- Task 1 and Task 2 share `package main` in one directory; sequenced in separate waves to avoid concurrent-edit conflicts.
- Task 3 references facts fixed by SPEC.md, not by Task 1's implementation choices, so parallel execution is safe; orchestrator still sanity-checks README claims against final Task 1 code before closing Task 3.
- The blocking `TaskCompleted` hook only runs `go test ./...` — `go vet`/`gofmt` compliance is **not** enforced automatically and must be checked explicitly by the orchestrator.

## 6. Resource allocation summary

| Task   | Initial profile | Complexity | Ambiguity | Risk | Dependencies |
|--------|------------------|------------|-----------|------|---------------|
| Task 1 | worker-sonnet    | low        | low       | low  | None          |
| Task 2 | worker-sonnet    | low        | low       | low  | Task 1        |
| Task 3 | worker-haiku     | low        | low       | low  | None          |

No task is assigned directly to `escalation-opus`.

## 7. Overall verification strategy

**Task-level:** `go build ./...`, `go vet ./...`, `gofmt -l .`, targeted `go test ./... -v`, manual curl checks.

**Repository completion gate:** `go test ./...` under the existing 300s timeout via `.claude/hooks/verify-unit-tests.sh`. Already correctly configured on this branch.

**Specification-level (post-integration, orchestrator-run):**
- Re-run `go build ./...`, `go test ./...`, `go vet ./...`, `gofmt -l .` (must be empty) together against the fully integrated tree.
- One live manual pass: run the binary, confirm startup log names the port, `curl` `GET /health`, `POST /health`, and an unknown route, checking status codes and bodies.
- Confirm `README.md` accurately reflects the final implementation.

No additional coverage, performance, or security gates are introduced.
