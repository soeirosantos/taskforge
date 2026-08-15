# Implementation Plan — Rust Health Check Service

Planning output for `SPEC.md`, produced under
`.claude/TASK_PLANNING_INSTRUCTIONS.md` and the bounded-execution policy in
`CLAUDE.md`.

---

## 1. Specification analysis

**Requested behavior.** A minimal Rust HTTP service exposing `GET /health` →
`200` + `Content-Type: application/json` + `{"status":"ok"}`; non-GET on
`/health` → `405`; unknown routes → `404`; port `8080` overridable by `PORT`,
with invalid `PORT` failing startup loudly rather than falling back; a startup
log line naming the configured port.

**Non-functional requirements.** Minimum dependency count; no `unsafe`; simple,
explicit Rust (no gratuitous macros, generics, traits, custom error frameworks);
simplest reasonable Cargo layout; human-auditable by an engineer with basic Rust
familiarity.

**Explicit constraints.** Stable toolchain; Cargo for everything; `cargo build`,
`cargo test`, `cargo fmt --all -- --check`, and
`cargo clippy --all-targets --all-features -- -D warnings` must all pass with
zero warnings; tests must not require a manually started server; prefer
exercising the router directly over binding a real port; no suppressing warnings
or weakening tests to pass.

**Invariants.** No `unsafe`. No dependency without a clear purpose (explicit
prohibition list in §9 of the spec: ORM, DI, config frameworks, persistence,
metrics, tracing, auth, non-JSON serialization, extra app frameworks). No
harness modification — everything under `.claude/` is off-limits to every
implementation task (`CLAUDE.md`).

**Success criteria.** The 13-item Definition of Done in spec §13.

**Out of scope.** DB, external services, authz, TLS, Docker/K8s, metrics,
tracing, graceful shutdown, additional endpoints, business logic, custom
protocol/runtime, benchmarking, performance work.

**Resolved by repository inspection.** Test command (`cargo test`), gate timeout
(300s), worker profiles and turn limits, the harness/application file boundary,
and the fact that no prior Rust conventions exist in this repository to conform
to — so the specification itself is the only style authority.

---

## 2. Repository and environment findings relevant to decomposition

| Finding | Planning consequence |
| --- | --- |
| No application code exists; the tree is harness-only | Greenfield Cargo project; Task 1 must be the sole Wave 1 task. |
| `rustfmt` and `clippy` are **not installed** (`rustup component add` needed; `/usr/local/rustup` and `/usr/local/cargo` are writable) | Two of the four spec-mandated checks cannot run today. Installation is folded into Task 1; it touches no repository file. |
| Empty cargo registry, crates.io reachable (index + static HTTP 200) | The first `cargo build` is the long pole and the biggest external-dependency risk. Front-loaded into Task 1 so it fails early. |
| `cargo test` exits **0 with zero tests** | The `TaskCompleted` gate is **vacuous** until Task 3 lands. Tasks 1–2 must be closed on their acceptance criteria plus task-specific verification. |
| A task leaving no `Cargo.toml` behind cannot close (`cargo test` exits 101 → gate refuses) | Task 1 must be self-sufficient and land a compiling crate. |
| `.gitignore` lacks `/target` | Task 1 adds it — root-level application file, permitted. |
| Toolchain is Rust 1.83.0 (aarch64); axum 0.8.9 has MSRV 1.80 | axum 0.8 is compatible. |
| Only `.claude/hooks/test-command.conf` differs from `main` (`TEST_COMMAND="cargo test"`, `TEST_TIMEOUT_SECONDS=300`) | Must stay that way. No task may touch `.claude/`. |

---

## 3. Task plan

### Task 1: Cargo project scaffold, dependency baseline, and verification toolchain

**Objective**

A compiling Cargo binary crate exists at the repository root with the final
dependency set resolved and vendored, and all four spec-mandated verification
commands are executable and green on the skeleton.

**Scope**

`Cargo.toml`, `Cargo.lock`, a placeholder `src/main.rs`, `.gitignore`.
Environment: `rustup component add rustfmt clippy`. No `.claude/` files.

**Dependencies** — None.

**Parallelizable** — No. Every other task depends on this.

**Complexity** medium · **Ambiguity** medium · **Risk** low

**Initial execution profile** — `worker-sonnet`

**Profile rationale** — Not mechanical: it requires choosing a minimal
dependency set that satisfies spec §9's prohibition list and simultaneously
supports §7's "test the router directly" requirement, then proving the choice
compiles. Haiku would treat this as a copy-paste scaffold and is likely to over-
or under-specify features.

**Implementation guidance**

- Binary crate, edition 2021, at the repository root. Spec §11: no
  `src/lib.rs`, no module tree, unless a later task demonstrates a need.
- Target dependency set — direct deps only, each with a stated purpose:
  - `axum` 0.8 — the HTTP stack (spec §9 explicitly permits Axum).
  - `tokio` 1 with the narrowest feature set that runs an axum server
    (`rt-multi-thread`, `net`, `macros`); do **not** use `features = ["full"]`.
  - `serde_json` 1 — JSON body construction. `serde` need not be a direct
    dependency if `serde_json::json!` is used.
  - dev-dependency `tower` 0.5 with `features = ["util"]` — supplies
    `ServiceExt::oneshot`, which is how Task 3 drives the `Router` without a
    socket.
- `axum::body::to_bytes` exists in 0.8, so **`http-body-util` is not needed** as
  a dev-dependency. Do not add it.
- Install the components with `rustup component add rustfmt clippy`. This is
  environment setup; it must not add config files, a `rust-toolchain.toml`, or
  any `.claude/` change.
- Commit `Cargo.lock` (spec §11 lists it). Add `/target` to `.gitignore`.
- `src/main.rs` at this stage is a placeholder that compiles cleanly under
  clippy with `-D warnings`; Task 2 replaces it. Do not implement the service
  here.

**Acceptance criteria**

- `Cargo.toml` declares only dependencies from the list above, each traceable to
  a specification requirement, with no entry from spec §9's prohibition list.
- `cargo build` completes with zero warnings from a cold registry.
- `cargo fmt --all -- --check` and
  `cargo clippy --all-targets --all-features -- -D warnings` both execute (i.e.
  the components are installed) and both pass.
- `Cargo.lock` exists and is tracked; `/target` is ignored.
- No file under `.claude/` is modified.

**Task-specific verification**

```bash
cargo build 2>&1 | tee /dev/stderr | grep -c warning   # expect 0
cargo fmt --all -- --check
cargo clippy --all-targets --all-features -- -D warnings
git status --porcelain .claude/   # expect only the pre-existing test-command.conf change
```

**Completion condition** — All four commands above succeed and the dependency
manifest is justified line by line in the worker's report. Note explicitly: the
`TaskCompleted` gate will pass here **vacuously** (zero tests), so it
contributes no evidence for this task.

**Escalation** — `worker-sonnet` → `escalation-opus` → STOP / human.
**Downstream of this task: Tasks 2, 3, 4, 5** — a halt here blocks the entire
plan.

---

### Task 2: Health service implementation — routing, responses, port configuration, startup logging

**Objective**

The service implements the full runtime behavior of the specification: the
health endpoint, the method and route error behaviors, `PORT` resolution with
loud failure on invalid input, and a startup log naming the configured port.

**Scope**

`src/main.rs`. No new dependencies. No test code (Task 3 owns that), but the
code must be *shaped* for testability.

**Dependencies** — Task 1

**Parallelizable** — No.

**Complexity** medium · **Ambiguity** medium · **Risk** medium

**Initial execution profile** — `worker-sonnet`

**Profile rationale** — Normal application implementation across a small,
well-bounded surface. The only genuine judgment calls are the port-validation
boundary and the router/`main` split, both specified below. No architectural
reasoning that would justify Opus.

**Implementation guidance**

- **Expose two testable units** so Task 3 need not bind a socket:
  - a function returning the configured `axum::Router` (e.g. `fn app() ->
    Router`), containing no I/O and no environment access;
  - a pure port-resolution function taking the raw variable value (e.g.
    `fn resolve_port(raw: Option<&str>) -> Result<u16, String>`) rather than
    reading `std::env` internally, so it is directly unit-testable.

  `main` composes the two, logs, binds, and serves. This is the one structural
  constraint the plan imposes; it exists because spec §7 requires server-free
  tests.
- **Routing.** Register `/health` with a GET-only method router. axum returns
  `405` for a mismatched method on a registered path and `404` for an
  unregistered path *by default* — do not hand-roll fallbacks or a custom `405`
  handler for behavior the framework already provides (spec §10, §14 "custom
  HTTP protocol implementation").
- **`HEAD /health` behaves like `GET`.** axum routes `HEAD` to the `GET` handler
  by default, and this is the intended behavior — spec §4's blanket "methods
  other than GET" wording is a specification miss, confirmed by the requester.
  Do not add a `405` override for `HEAD`.
- **Response.** Return `axum::Json` so `Content-Type: application/json` is set
  by the framework rather than by a manual header insertion. Body exactly
  `{"status":"ok"}`.
- **Port validation.** Parse as `u16`. Reject anything that does not parse, and
  reject `0` (binding port 0 yields an OS-assigned port, which would make the
  startup log line — a spec §6 requirement — untrue). Treat an empty or
  whitespace-only `PORT` as invalid, not as unset. An **unset** `PORT` uses
  `8080`.
- **Failure mode.** "Fail clearly" (spec §2) means: a descriptive message on
  stderr naming the offending value and the expected range, and a non-zero exit.
  Prefer an explicit message plus `std::process::exit(1)` over an `unwrap()`
  panic with a backtrace. Do **not** fall back to 8080.
- **Logging.** `println!`/`eprintln!` only. Spec §6 and §14 rule out a logging
  or tracing framework for an application this size.
- No `unsafe`. No custom error type or error framework — a `String` or
  `std::process::exit` at the boundary is sufficient and is what spec §10 asks
  for.

**Acceptance criteria**

- `GET /health` returns `200`, `Content-Type: application/json`, body
  `{"status":"ok"}`.
- `POST /health` returns `405`.
- `HEAD /health` returns `200` (same handler as `GET`).
- `GET /unknown` returns `404`.
- With `PORT` unset the server binds `8080`; with `PORT=9000` it binds `9000`;
  the startup message names the port actually bound.
- An invalid `PORT` (non-numeric, out of `u16` range, `0`, or empty) terminates
  startup with a non-zero exit and an explanatory stderr message, and never
  binds `8080`.
- No `unsafe` appears anywhere in the crate.

**Task-specific verification**

```bash
cargo build && cargo clippy --all-targets --all-features -- -D warnings && cargo fmt --all -- --check
grep -rn "unsafe" src/ || echo "no unsafe"

# runtime smoke, torn down after each check:
cargo run & sleep 2
curl -isS localhost:8080/health
curl -o /dev/null -w '%{http_code}\n' -X POST localhost:8080/health
curl -o /dev/null -w '%{http_code}\n' localhost:8080/unknown
kill %1

PORT=9000 cargo run & sleep 2; curl -sS localhost:9000/health; kill %1
PORT=abc cargo run; echo "exit=$?"   # expect non-zero, message on stderr
```

**Completion condition** — Every acceptance criterion demonstrated by the
observed command output above, pasted into the report. The gate remains vacuous
at this point (still no tests); it must not be cited as evidence.

**Escalation** — `worker-sonnet` → `escalation-opus` → STOP / human.
**Downstream: Tasks 3, 4, 5.**

---

### Task 3: Automated test suite

**Objective**

`cargo test` deterministically verifies the specification's five required
behaviors plus port resolution, without binding a network port or requiring a
manually started server — converting the `TaskCompleted` gate from vacuous to
meaningful.

**Scope**

Test code only: `#[cfg(test)] mod tests` in `src/main.rs`. No changes to
production behavior. If a signature genuinely blocks testability, adjusting it
is in scope; changing observable service behavior is not.

**Dependencies** — Task 2

**Parallelizable** — Yes, alongside Task 4 (disjoint files: test module vs.
`README.md`).

**Complexity** medium · **Ambiguity** low · **Risk** medium

**Initial execution profile** — `worker-sonnet`

**Profile rationale** — Unit-test implementation is squarely Sonnet's remit. The
`tower::ServiceExt::oneshot` + `axum::body::to_bytes` idiom requires real
knowledge of the axum 0.8 API and is beyond mechanical editing. Risk is medium
because this suite becomes the repository's completion gate for every subsequent
task — a weak suite silently degrades the apparatus.

**Implementation guidance**

- Drive the `Router` from Task 2 through `tower::ServiceExt::oneshot` with
  `http::Request`s. Do **not** spawn a server, bind a listener, or `sleep`
  (spec §7).
- Read bodies with `axum::body::to_bytes`; do not add `http-body-util`.
- Required cases (spec §7): `GET /health` → `200`; response `Content-Type` is a
  JSON content type; body parses to `{"status":"ok"}`; `POST /health` → `405`;
  unknown route → `404`.
- Assert the body by parsing it as JSON and comparing values — not by string
  equality against a serialization, which is brittle to key spacing.
- Additionally unit-test `resolve_port`: unset → `8080`; `"9000"` → `9000`;
  `"abc"`, `"70000"`, `"0"`, `""` → error. This is the only coverage the DoD's
  "invalid PORT configuration fails clearly" gets inside `cargo test`; the
  process-exit behavior itself is covered by Task 5's runtime check.
- Tests must be clippy-clean under `--all-targets -D warnings`.
- Tests as written define the gate for the rest of the project: no `#[ignore]`,
  no assertions weakened to accommodate the implementation. If a test fails, fix
  `src/main.rs`.

**Acceptance criteria**

- `cargo test` passes and reports **at least 5** executed tests covering the
  five spec §7 behaviors, plus the port-resolution cases.
- No test binds a TCP port, spawns the binary, or depends on a running server.
- Each of the five spec §7 requirements maps to a named, identifiable test.
- `cargo clippy --all-targets --all-features -- -D warnings` still passes.

**Task-specific verification**

```bash
cargo test -- --nocapture   # inspect the per-test list, not just the summary
cargo clippy --all-targets --all-features -- -D warnings
cargo fmt --all -- --check
```

**Completion condition** — The `cargo test` output showing named passing tests,
pasted into the report, with each spec §7 requirement mapped to a test name.
From this task onward the `TaskCompleted` gate carries real signal.

**Escalation** — `worker-sonnet` → `escalation-opus` → STOP / human.
**Downstream: Task 5.**

---

### Task 4: README documentation

**Objective**

A short root `README.md` covering the seven items in spec §12.

**Scope**

`README.md` at the repository root only. `CLAUDE.md` confirms the root
`README.md` is the application's and collides with nothing in the harness.

**Dependencies** — Task 2 (the documented behavior must describe what was
actually built).

**Parallelizable** — Yes, alongside Task 3.

**Complexity** low · **Ambiguity** low · **Risk** low

**Initial execution profile** — `worker-haiku`

**Profile rationale** — Transcription of already-determined facts into a fixed
seven-item outline. No design decisions remain once Task 2 is done.

**Implementation guidance**

- Cover exactly: purpose; prerequisites (stable Rust toolchain, and note that
  `rustfmt`/`clippy` components are required for the static checks); how to run;
  how to test; how to run static verification (all four commands verbatim from
  spec §8); how to call the endpoint; how to override the port.
- Document the actual `PORT` validation rule implemented in Task 2, including
  which values are rejected — do not restate the spec if the implementation is
  narrower or broader.
- Keep it short. Do not add badges, architecture diagrams, contribution
  guidelines, or a license section (spec §9/§14 spirit: nothing not requested).
- Do not document the `.claude/` harness; that is `AGENT_CONTROL_README.md`'s
  job.

**Acceptance criteria**

- `README.md` exists at the repository root and addresses all seven spec §12
  items.
- Every command it shows runs successfully as written against the current tree.
- The stated `PORT` behavior matches the implementation.

**Task-specific verification** — Copy each command block out of the README and
execute it; all must succeed. `cargo fmt`/`clippy` unaffected (Markdown only).

**Completion condition** — The transcript of each README command executed
successfully, plus a spec §12 item-by-item checklist. The gate must also pass
(it is meaningful once Task 3 has landed; if Task 3 has not yet landed, the
orchestrator records that Task 4's gate pass was vacuous).

**Escalation** — `worker-haiku` → `escalation-opus` → STOP / human.
**Downstream: Task 5** (documentation-only; a halt here does not block runtime
correctness, but it does block the spec §13 DoD).

---

### Task 5: Specification conformance verification

**Objective**

Deterministic, end-to-end evidence that every one of the 13 Definition-of-Done
items in spec §13 holds on the integrated tree — including the runtime behaviors
that `cargo test` structurally cannot cover.

**Scope**

Execution and evidence collection only. **This task must not modify any file.**
Failures are reported back to the orchestrator, which reopens the responsible
task.

**Dependencies** — Tasks 3 and 4

**Parallelizable** — No.

**Complexity** low · **Ambiguity** low · **Risk** low

**Initial execution profile** — `worker-haiku`

**Profile rationale** — Running a fixed command list and reporting real output
verbatim is exactly the "command execution / deterministic evidence gathering"
profile. Assigning reasoning-capable models here would invite
fixing-while-verifying, which is precisely the self-certification this harness
exists to prevent.

**Implementation guidance**

- Run each check and record its actual output. Do not summarize away failures;
  do not fix anything.
- The four static commands from spec §8, then the runtime checks the test suite
  cannot make: default-port bind, `PORT=9000` override, and invalid-`PORT`
  non-zero exit with a message.
- Confirm no `unsafe` in the crate and that `.claude/` is untouched apart from
  the pre-existing `test-command.conf` change.
- Report a per-item pass/fail table against spec §13's 13 items.

**Acceptance criteria**

- All four spec §8 commands succeed with zero warnings.
- Runtime checks confirm: `GET /health` → `200` + JSON + `{"status":"ok"}`;
  `POST /health` → `405`; unknown route → `404`; default `8080`; `PORT=9000`
  honored; invalid `PORT` exits non-zero with a clear message.
- No `unsafe`; `git status --porcelain .claude/` shows only
  `test-command.conf`.
- A 13-row table maps each spec §13 item to concrete observed evidence.

**Task-specific verification** — The full spec §8 command set plus the Task 2
runtime smoke sequence, rerun against the integrated tree.

**Completion condition** — The complete evidence table with real command output.
**The orchestrator re-runs this entire command set itself** before closing — per
`CLAUDE.md`, a worker's report is not sufficient evidence.

**Escalation** — `worker-haiku` → `escalation-opus` → STOP / human. A failure
here normally means reopening Task 2, 3, or 4 rather than escalating Task 5
itself; escalate Task 5 only if the *verification* cannot be executed.
**Downstream: none.**

---

## 4. Execution graph and waves

```text
Task 1 (scaffold + toolchain + deps)
   │
Task 2 (service implementation)
   │
   ├── Task 3 (test suite) ──┐
   │                          ├── Task 5 (spec conformance)
   └── Task 4 (README) ──────┘
```

```text
Wave 1:  Task 1

Wave 2:  Task 2

Wave 3:  Task 3
         Task 4

Wave 4:  Task 5
```

Only Wave 3 parallelizes. Tasks 3 and 4 touch disjoint files (`src/main.rs` test
module vs. `README.md`) and neither changes service behavior, so concurrent
execution carries no integration risk. Waves 1 and 2 are strictly serial: the
whole repository is one small crate, and any concurrency there would put two
workers in the same file.

---

## 5. Risk and ambiguity summary

### Highest-risk tasks

**Task 3 (test suite)** — highest leverage in the plan. This suite *becomes* the
`TaskCompleted` gate for everything after it; a superficial suite (e.g.
asserting only status codes, or `#[ignore]`-ing a hard case) would leave the
gate looking green while measuring nothing. Additional deterministic
verification available: `cargo test -- --nocapture` exposes the per-test list,
so the orchestrator can confirm the five spec §7 behaviors are individually
named and executed rather than trusting a summary count.

**Task 2 (implementation)** — carries all externally observable behavior. Well
mitigated: every criterion is checkable by `curl` and exit code before any test
exists.

**Task 1 (scaffold)** — low intrinsic risk but total blast radius. It is the
only task with an external dependency (crates.io) and the only one modifying the
toolchain.

### Material assumptions

1. **Port `0` and empty `PORT` are invalid.** The specification does not
   enumerate what "invalid" means. Rejecting `0` follows from spec §6: binding
   port 0 yields an OS-assigned port, which would make the mandated startup log
   line false. Rejecting empty follows from spec §2's "must not silently fall
   back". Documented, low risk, trivially reversible.
2. **"Fail clearly" = descriptive stderr message + non-zero exit.** Chosen over
   a raw `unwrap()` panic for auditability (spec §10).
3. **`HEAD /health` succeeds and behaves like `GET`.** *(Resolved — not an open
   assumption.)* axum routes `HEAD` to the `GET` handler by default. Spec §4's
   "methods other than GET … must return 405" is a specification miss; the
   requester confirmed HEAD should behave like GET, which is also correct HTTP
   semantics (HEAD is GET without a body). No override is implemented.
4. **Tests live in `src/main.rs` under `#[cfg(test)]`, not `tests/`.** A binary
   crate exposes nothing to an integration test without adding a `lib` target;
   spec §11 says `tests/` only "if needed" and warns against unnecessary layers.
5. **Network access to crates.io persists** for the duration of Task 1. Verified
   reachable at planning time.
6. **The 300s gate timeout accommodates a cold `cargo test`.** Unmeasured. See
   cross-task risks.

### Unresolved ambiguities

*None that prevent implementation.* All of the following are handled by
documented assumption:

- Precise definition of an invalid `PORT` (assumption 1).
- Exact failure mechanism for invalid `PORT` (assumption 2).
- Log destination (stdout vs. stderr) — the spec calls the format
  implementation-specific; either satisfies spec §6.

The `HEAD` question that was open at planning time has been resolved by the
requester and is recorded as assumption 3.

### Cross-task risks

- **`src/main.rs` is shared by Tasks 2 and 3.** Mitigated by strict ordering
  (never concurrent) and by Task 3 being confined to the `#[cfg(test)]` module.
- **Task 1's dependency choices bind Task 3's testing strategy.** If Task 1
  omits the `tower` dev-dependency or the `util` feature, Task 3 cannot use
  `oneshot` and will be tempted to bind a real port — violating spec §7. Task
  1's acceptance criteria name the dev-dependency explicitly for this reason.
- **Gate vacuity in Waves 1–2.** `cargo test` exits 0 with zero tests, so the
  `TaskCompleted` hook passes without evidence for Tasks 1 and 2. The
  orchestrator must close those two on their task-specific verification output
  and record explicitly that the gate was uninformative. This is the single most
  likely way for this plan to produce a false-green result.
- **Cold-build time vs. the 300s timeout.** The gate compiles the crate and its
  dependency tree on its first run, and `record-task-event.sh` re-runs the suite
  immediately afterward (cached, so cheap). If the cold `cargo test` approaches
  300s the gate will terminate it and refuse completion. `TEST_TIMEOUT_SECONDS`
  lives in `.claude/hooks/test-command.conf`, which **no implementation task may
  modify** — if this fires, it is an orchestrator/human decision, not a
  worker's. Warming the build in Task 1 makes it unlikely.
- **Toolchain mutation in Task 1.** `rustup component add` changes shared state
  at `/usr/local/rustup`. It adds no repository file and does not touch
  `.claude/`, so it does not affect the branch-comparability invariant — but the
  orchestrator should note it, since a differently-provisioned experiment arm
  would not have it.
- **Uncommitted gate config.** `test-command.conf` is modified but uncommitted;
  `check-arm-ready.sh` treats this as an apparatus warning. Committing it is a
  harness decision for the human operator, not work for any task in this plan.

---

## 6. Resource allocation summary

| Task | Title | Initial profile | Complexity | Ambiguity | Risk | Dependencies |
| --- | --- | --- | --- | --- | --- | --- |
| 1 | Scaffold, dependencies, toolchain | `worker-sonnet` | medium | medium | low | None |
| 2 | Service implementation | `worker-sonnet` | medium | medium | medium | Task 1 |
| 3 | Automated test suite | `worker-sonnet` | medium | low | medium | Task 2 |
| 4 | README | `worker-haiku` | low | low | low | Task 2 |
| 5 | Spec conformance verification | `worker-haiku` | low | low | low | Tasks 3, 4 |

**No task is assigned directly to `escalation-opus`.** Nothing in this
specification meets the bar in `.claude/TASK_PLANNING_INSTRUCTIONS.md` §4 for
direct Opus assignment: there is no substantial architectural reasoning, no
cross-cutting change, no concurrency or security-sensitive behavior, and no high
ambiguity. The service is a single file with three routes. Opus remains
available as the single bounded escalation tier for any of the five tasks per
`CLAUDE.md`.

---

## 7. Overall verification strategy

**Task-level (fast, targeted).** `cargo build` →
`cargo clippy --all-targets --all-features -- -D warnings` →
`cargo fmt --all -- --check` → `cargo test` (from Task 3 onward), plus per-task
`curl`/exit-code smoke checks. This follows the instruction's evidence ordering:
compiler first, then lints, then tests. Rust's type checking is subsumed by
`cargo build`; there is no separate type-check layer, and no security tooling
exists in this repository — neither is invented here.

**Repository completion gate.** `.claude/hooks/verify-unit-tests.sh` running
`TEST_COMMAND="cargo test"` under a 300s self-enforced timeout. It fails closed
and can only refuse completion, never certify it. It is **vacuous for Tasks 1
and 2** and meaningful from Task 3 onward. The orchestrator runs the
verification itself after each worker returns rather than trusting the worker's
report, and it alone calls `TaskCreate`/`TaskUpdate`.

**Specification-level.** Task 5's 13-row Definition-of-Done evidence table,
re-run by the orchestrator on the integrated tree. This is the only layer that
covers the three DoD items `cargo test` structurally cannot reach: the
default-port bind, the live `PORT` override, and the invalid-`PORT` process
exit.

No new quality gates — no coverage thresholds, no performance budgets, no
integration-test tiers — are introduced. The specification requires none, and
`.claude/TASK_PLANNING_INSTRUCTIONS.md` §7 forbids inventing them.
