# Go Health Check Service — Smoke Test Specification

## 1. Objective

Implement a minimal HTTP service in Go exposing a health-check endpoint.

This project is intended as a smoke test for an automated coding-agent workflow. Keep the implementation intentionally small and straightforward.

Do not add functionality that is not required by this specification.

---

## 2. Technical Requirements

* Language: Go
* Use the Go standard library where practical.
* The application must be runnable locally with:

```bash
go run .
```

* The application must compile with:

```bash
go build ./...
```

* The default HTTP port must be:

```text
8080
```

The port may optionally be overridden using the environment variable:

```text
PORT
```

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

---

## 7. Tests

Automated tests are required.

At minimum, verify:

1. `GET /health` returns `200`.
2. The response is JSON.
3. The JSON response contains:

```json
{
  "status": "ok"
}
```

4. `POST /health` returns `405`.
5. An unknown route returns `404`.

Tests should run with:

```bash
go test ./...
```

Tests must not require the developer to manually start the HTTP server.

---

## 8. Static Verification

The completed implementation must pass:

```bash
go test ./...
go vet ./...
gofmt
go build ./...
```

Source files must already be correctly formatted; verification must not depend on formatting files after implementation is declared complete.

Do not suppress verification failures.

---

## 9. Project Structure

Use the simplest reasonable project structure.

Do not introduce unnecessary:

* frameworks
* routers
* dependency-injection libraries
* configuration libraries
* persistence
* Docker configuration
* middleware
* metrics systems
* authentication
* additional endpoints

The Go standard library is sufficient for this implementation.

---

## 10. Documentation

Provide a short `README.md` containing:

* purpose of the application
* how to run it
* how to run tests
* how to run verification
* how to call the health endpoint

Example request:

```bash
curl http://localhost:8080/health
```

---

## 11. Definition of Done

The task is complete only when all of the following are true:

```text
✓ go build ./... succeeds
✓ go test ./... succeeds
✓ go vet ./... succeeds
✓ source code is gofmt-compliant
✓ GET /health returns HTTP 200
✓ GET /health returns {"status":"ok"}
✓ POST /health returns HTTP 405
✓ unknown routes return HTTP 404
✓ README documents execution and verification
```

No verification step may be skipped or weakened to satisfy the definition of done.

---

## 12. Out of Scope

Do not implement:

* database access
* external services
* authentication or authorization
* TLS
* Docker
* Kubernetes
* Prometheus metrics
* tracing
* graceful shutdown logic
* readiness or liveness endpoints beyond `/health`
* application-specific business logic

---

## 13. Implementation Instruction

Plan and implement the system described above according to the configured task-planning and bounded-agent execution procedures.

Prefer the smallest implementation that completely satisfies the specification.

Use deterministic verification to determine completion.

If all required checks pass, stop execution rather than introducing additional improvements or functionality.
