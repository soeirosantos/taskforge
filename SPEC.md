# Rust Health Check Service — Smoke Test Specification

## 1. Objective

Implement a minimal HTTP service in Rust exposing a health-check endpoint.

This project is intended as a smoke test for an automated coding-agent workflow. Keep the implementation intentionally small and straightforward.

Do not add functionality that is not required by this specification.

---

## 2. Technical Requirements

* Language: Rust
* Use the stable Rust toolchain.
* Use Cargo for dependency management, build, test, and verification.
* A small, mature HTTP library or framework may be used.
* Prefer the minimum number of dependencies necessary to implement the service.
* Do not use `unsafe` code.

The application must be runnable locally with:

```bash
cargo run
```

The application must compile with:

```bash
cargo build
```

The default HTTP port must be:

```text
8080
```

The port may optionally be overridden using the environment variable:

```text
PORT
```

Invalid `PORT` values must cause startup to fail clearly rather than silently falling back to the default.

---

## 3. Health Endpoint

Expose:

```text
GET /health
```

A successful request must return:

```text
HTTP 200 OK
```

with:

```text
Content-Type: application/json
```

and the following JSON body:

```json
{
  "status": "ok"
}
```

No authentication is required.

---

## 4. Unsupported Methods

Requests to `/health` using methods other than `GET` must return:

```text
405 Method Not Allowed
```

For example:

```text
POST /health
```

must not return a successful health response.

---

## 5. Unknown Routes

Requests to undefined routes must return:

```text
404 Not Found
```

For example:

```text
GET /unknown
```

must return `404`.

---

## 6. Logging

On startup, log that the HTTP server is starting and include the configured port.

Example semantics:

```text
server starting on port 8080
```

The exact log format is implementation-specific.

Do not introduce a logging framework unless it provides a clear benefit for this minimal application.

---

## 7. Tests

Automated tests are required.

At minimum, verify:

1. `GET /health` returns `200`.
2. The response has a JSON content type.
3. The JSON response contains:

```json
{
  "status": "ok"
}
```

4. `POST /health` returns `405`.
5. An unknown route returns `404`.

Tests must run with:

```bash
cargo test
```

Tests must not require the developer to manually start the HTTP server.

Prefer testing the HTTP application/router directly rather than binding to a real network port when the selected HTTP library supports this cleanly.

---

## 8. Static Verification

The completed implementation must pass all of the following:

```bash
cargo build
cargo test
cargo fmt --all -- --check
cargo clippy --all-targets --all-features -- -D warnings
```

No compiler warnings, Clippy warnings, formatting failures, or test failures may remain.

Do not suppress warnings merely to make verification pass unless there is a documented technical justification.

Do not weaken or remove tests to satisfy the definition of done.

---

## 9. Dependency Requirements

Keep dependencies minimal.

A mature Rust HTTP library/framework is permitted because the Rust standard library does not provide a complete HTTP server abstraction suitable for this task.

Acceptable implementation choices include a small conventional HTTP stack such as Axum or an equivalent mature library.

Do not add dependencies for capabilities that can be implemented clearly with the standard library.

Every direct dependency must have a clear purpose.

The implementation must not introduce unnecessary:

* ORM libraries
* dependency-injection frameworks
* configuration frameworks
* persistence libraries
* metrics libraries
* tracing systems
* authentication libraries
* serialization formats other than JSON
* application frameworks beyond what is needed to expose the HTTP service

---

## 10. Code Quality and Human Auditability

Prefer simple, explicit Rust.

Avoid unnecessary:

* macros
* advanced generic abstractions
* custom traits
* complex lifetime relationships
* type-level programming
* metaprogramming
* deeply nested combinators
* excessive iterator chaining
* custom error frameworks

Use Rust's type system where it naturally improves correctness, but do not introduce abstractions solely to demonstrate language features.

The implementation should be understandable by an engineer with basic Rust familiarity.

No `unsafe` code is permitted.

---

## 11. Project Structure

Use the simplest reasonable Cargo project structure.

A minimal implementation may contain only:

```text
Cargo.toml
Cargo.lock
src/
tests/        # if needed
README.md
```

Additional modules should be introduced only when they provide a clear separation of concerns or improve testability.

Do not create architectural layers that are unnecessary for this application.

---

## 12. Documentation

Provide a short `README.md` containing:

* purpose of the application
* prerequisites
* how to run it
* how to run tests
* how to run static verification
* how to call the health endpoint
* how to override the port

Example:

```bash
cargo run
```

Example request:

```bash
curl http://localhost:8080/health
```

Expected response:

```json
{"status":"ok"}
```

Example custom port:

```bash
PORT=9000 cargo run
```

---

## 13. Definition of Done

The task is complete only when all of the following are true:

```text
✓ cargo build succeeds
✓ cargo test succeeds
✓ cargo fmt --all -- --check succeeds
✓ cargo clippy --all-targets --all-features -- -D warnings succeeds
✓ no unsafe code is present
✓ GET /health returns HTTP 200
✓ GET /health returns {"status":"ok"}
✓ response content type is JSON
✓ POST /health returns HTTP 405
✓ unknown routes return HTTP 404
✓ PORT overrides the default port
✓ invalid PORT configuration fails clearly
✓ README documents execution and verification
```

No verification step may be skipped, weakened, disabled, or bypassed to satisfy the definition of done.

---

## 14. Out of Scope

Do not implement:

* database access
* external services
* authentication or authorization
* TLS
* Docker
* Kubernetes
* Prometheus metrics
* distributed tracing
* graceful shutdown logic
* readiness or liveness endpoints beyond `/health`
* application-specific business logic
* custom HTTP protocol implementation
* custom async runtime
* benchmarking
* performance optimization beyond avoiding obviously inefficient implementation choices

---

## 15. Implementation Instruction

Plan and implement the system described above according to the configured task-planning and bounded-agent execution procedures.

Prefer the smallest implementation that completely satisfies the specification.

Use the Rust compiler, test suite, formatter, and Clippy as deterministic verification mechanisms throughout implementation.

Do not introduce advanced Rust constructs unless they are necessary to satisfy a requirement.

If all required checks pass, stop execution rather than introducing additional improvements, abstractions, dependencies, or functionality.
