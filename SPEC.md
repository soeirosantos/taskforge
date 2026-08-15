# TaskForge — Go API Job Processing Service

## 1. Objective

Build a small asynchronous job-processing system in Go consisting of:

1. A REST API
2. A persistent SQLite job store
3. A concurrent worker pool

The system accepts jobs through the REST API, processes them asynchronously, persists their state, and allows API clients to query, cancel, and retry jobs.

The implementation must prioritize:

* correctness
* deterministic behavior
* concurrency safety
* testability
* machine-verifiable constraints
* straightforward, human-readable code

Do not add functionality that is not required by this specification.

---

# 2. Specification Authority and Implementation Freedom

This specification defines the required externally observable behavior and correctness guarantees.

Do not ask for design preferences that are intentionally left to the implementation.

When an implementation detail is not specified:

1. choose the simplest idiomatic Go solution;
2. do not expand scope;
3. do not weaken any stated invariant;
4. document any materially important design decision in the README.

Ask for clarification only if two explicit requirements in this specification cannot simultaneously be satisfied.

The implementation must not modify the surrounding task-planning, agent-orchestration, telemetry, sandboxing, or bounded-execution environment.

---

# 3. Technical Requirements

Use:

* Go
* the stable Go toolchain already available in the execution environment
* Go modules
* `net/http` or another standard-library HTTP facility where practical
* `database/sql`
* SQLite
* `modernc.org/sqlite` as the SQLite driver
* `log/slog` for application logging

Do not introduce a web framework unless a standard-library implementation would materially complicate correctness.

Do not use CGO.

Do not install or switch Go toolchain versions.

The application must run with:

```bash
go run .
```

The project must build with:

```bash
go build ./...
```

---

# 4. Configuration

The application supports exactly these runtime configuration values.

## HTTP Port

Environment variable:

```text
PORT
```

Default:

```text
8080
```

Valid values:

```text
1-65535
```

Invalid values must cause startup to fail with a non-zero exit status and a clear error message.

---

## Worker Count

Environment variable:

```text
WORKER_COUNT
```

Default:

```text
4
```

Valid values:

```text
1-64
```

Invalid values must cause startup to fail.

---

## Database Path

Environment variable:

```text
DATABASE_PATH
```

Default:

```text
./taskforge.db
```

An explicitly supplied empty value is invalid and must cause startup to fail.

---

## Shutdown Timeout

Use a shutdown timeout of:

```text
10 seconds
```

No environment variable is required for this value in version 1.

---

# 5. SQLite Runtime Behavior

Use `database/sql`.

For version 1, configure the SQLite database to use a single open connection:

```go
db.SetMaxOpenConns(1)
```

This requirement exists to keep SQLite write behavior deterministic under concurrent application activity.

Database initialization must happen automatically.

No manual SQL commands may be required before application startup.

The application must create any required tables or indexes if they do not already exist.

Schema migrations beyond what is necessary for initial schema creation are not required.

---

# 6. Supported Job Types

The system supports exactly three job types:

```text
hash
delay
fail
```

Job type names are case-sensitive.

Any other value is invalid.

---

# 7. Hash Job

Request payload:

```json
{
  "type": "hash",
  "payload": {
    "text": "hello world"
  }
}
```

`payload` must contain exactly one field:

```text
text
```

`text` must be a JSON string.

An empty string is valid.

The operation calculates SHA-256 over the UTF-8 bytes of the string.

Successful result:

```json
{
  "sha256": "b94d27b9934d3e08a52e52d7da7dabfac484efe37a5380ee9088f7ace2efcde9"
}
```

The SHA-256 value must be lowercase hexadecimal.

---

# 8. Delay Job

Request payload:

```json
{
  "type": "delay",
  "payload": {
    "milliseconds": 5000
  }
}
```

`payload` must contain exactly one field:

```text
milliseconds
```

The value must be an integer satisfying:

```text
100 <= milliseconds <= 30000
```

The job waits for approximately the requested duration unless cancelled.

Successful result:

```json
{
  "delayed_milliseconds": 5000
}
```

A running delay job must observe cooperative cancellation promptly rather than waiting for the full delay duration.

Use Go cancellation primitives such as `context.Context`.

---

# 9. Fail Job

Request payload:

```json
{
  "type": "fail",
  "payload": {}
}
```

The payload must be an empty JSON object.

The job always fails.

Its persisted error must be:

```json
{
  "code": "INTENTIONAL_FAILURE",
  "message": "job failed intentionally"
}
```

This job exists to exercise failure and retry behavior.

---

# 10. JSON Input Rules

For `POST /jobs`:

* `Content-Type` must be `application/json`.
* Parameters such as `charset=utf-8` are permitted.
* The request body must contain exactly one JSON object.
* Trailing non-whitespace data is invalid.
* Unknown JSON fields must be rejected.
* Missing required fields must be rejected.
* Wrong JSON field types must be rejected.
* Job-specific payloads must reject unknown fields.

Malformed or invalid job input returns:

```text
400 Bad Request
```

Unsupported content type returns:

```text
415 Unsupported Media Type
```

No explicit request-body size limit is required for version 1.

---

# 11. Job Representation

Every API job object must use this representation:

```json
{
  "id": "string",
  "type": "hash",
  "status": "QUEUED",
  "payload": {},
  "result": null,
  "error": null,
  "attempt_count": 0,
  "created_at": "2026-01-01T12:00:00Z",
  "queued_at": "2026-01-01T12:00:00Z",
  "started_at": null,
  "finished_at": null,
  "updated_at": "2026-01-01T12:00:00Z"
}
```

Required fields:

```text
id
type
status
payload
result
error
attempt_count
created_at
queued_at
started_at
finished_at
updated_at
```

Job IDs must be UUIDs.

Timestamp values must:

* use UTC;
* use RFC 3339-compatible JSON strings;
* contain sufficient precision to preserve ordering generated by the implementation.

Nullable timestamp fields must be represented as JSON `null`.

`result` must be `null` unless the job is `COMPLETED`.

`error` must be `null` unless the job is `FAILED`.

A `CANCELLED` job has:

```json
{
  "result": null,
  "error": null
}
```

---

# 12. Job States

A job is always in exactly one state:

```text
QUEUED
RUNNING
COMPLETED
FAILED
CANCELLED
```

Valid transitions are:

```text
QUEUED  -> RUNNING
QUEUED  -> CANCELLED

RUNNING -> COMPLETED
RUNNING -> FAILED
RUNNING -> CANCELLED

FAILED  -> QUEUED
```

No other transition is valid.

Terminal states are:

```text
COMPLETED
CANCELLED
```

`FAILED` may be retried.

---

# 13. Timestamp Semantics

At creation:

```text
created_at = now
queued_at = now
started_at = null
finished_at = null
updated_at = now
```

When claimed:

```text
started_at = now
finished_at = null
updated_at = now
```

When execution completes, fails, or is cancelled:

```text
finished_at = now
updated_at = now
```

On retry:

```text
queued_at = now
started_at = null
finished_at = null
result = null
error = null
updated_at = now
```

`created_at` never changes.

For the current execution cycle:

```text
created_at <= queued_at
```

and, where values exist:

```text
queued_at <= started_at <= finished_at
```

---

# 14. Attempt Count

A newly created job has:

```text
attempt_count = 0
```

The count increments exactly once when a worker successfully transitions:

```text
QUEUED -> RUNNING
```

The state transition and increment must occur atomically.

Retrying a failed job does not itself increment the count.

Maximum:

```text
attempt_count = 3
```

A job with three completed execution attempts may not be retried.

Workers must never claim a job whose `attempt_count` is already `3`.

---

# 15. Core State Invariants

The following invariants must always hold.

A `COMPLETED` job can never leave `COMPLETED`.

A `CANCELLED` job can never leave `CANCELLED`.

A successful cancellation guarantees that `CANCELLED` is the final state.

A job must never execute concurrently more than once.

At most one worker may own a job at a given moment.

All competing state transitions must be implemented atomically.

Application-level read-then-write logic without a concurrency-safe conditional update or equivalent synchronization is not sufficient.

---

# 16. REST API

The service exposes:

```text
POST /jobs
GET  /jobs
GET  /jobs/{id}
POST /jobs/{id}/cancel
POST /jobs/{id}/retry
GET  /health
```

All API responses that contain a body must use:

```text
Content-Type: application/json
```

---

# 17. Create Job

Endpoint:

```text
POST /jobs
```

Successful response:

```text
201 Created
```

The response contains the newly created job snapshot.

That snapshot must show:

```text
status = QUEUED
attempt_count = 0
```

The response represents the creation transaction itself.

A worker is allowed to claim the job immediately afterward, so a subsequent `GET` may already show another state.

Input validation must complete before the job becomes visible to workers.

---

# 18. Get Job

Endpoint:

```text
GET /jobs/{id}
```

Success:

```text
200 OK
```

Returns the current complete job object.

Unknown ID:

```text
404 Not Found
```

Malformed UUID:

```text
404 Not Found
```

---

# 19. List Jobs

Endpoint:

```text
GET /jobs
```

Response:

```json
{
  "jobs": []
}
```

No pagination is required.

Optional filters:

```text
status
type
```

Examples:

```text
GET /jobs?status=FAILED
GET /jobs?type=hash
GET /jobs?status=COMPLETED&type=hash
```

Valid `status` values are exactly:

```text
QUEUED
RUNNING
COMPLETED
FAILED
CANCELLED
```

Valid `type` values are exactly:

```text
hash
delay
fail
```

Invalid filter values return:

```text
400 Bad Request
```

Jobs must be ordered by:

```text
created_at descending
```

with ties broken by:

```text
id ascending
```

---

# 20. Worker Queue Ordering

Workers must claim queued jobs in:

```text
queued_at ascending
```

order.

If two jobs have the same `queued_at`, break the tie using:

```text
id ascending
```

A retried job receives a new `queued_at` value and therefore re-enters the queue at that time.

---

# 21. Atomic Job Claiming

A worker may execute a job only after atomically transitioning it:

```text
QUEUED -> RUNNING
```

The same atomic operation must:

1. verify `attempt_count < 3`;
2. increment `attempt_count`;
3. set `started_at`;
4. update `updated_at`.

Two workers competing for the same queued job must not both succeed.

Execution must not begin until the `RUNNING` state has been persisted.

---

# 22. Successful Execution

When execution succeeds, the worker attempts the atomic transition:

```text
RUNNING -> COMPLETED
```

If successful, persist:

* result;
* `finished_at`;
* `updated_at`.

The transition must succeed only if the persisted job is still `RUNNING`.

If another valid transition has already changed the job from `RUNNING`, the worker must not overwrite that state.

---

# 23. Failed Execution

When execution fails, the worker attempts:

```text
RUNNING -> FAILED
```

If successful, persist:

* execution error;
* `finished_at`;
* `updated_at`.

The transition must succeed only if the persisted job is still `RUNNING`.

A job failure must not terminate the worker pool.

Jobs do not retry automatically.

---

# 24. Cancel Job

Endpoint:

```text
POST /jobs/{id}/cancel
```

Cancellation is valid from:

```text
QUEUED
RUNNING
```

Successful cancellation performs an atomic transition to:

```text
CANCELLED
```

and returns:

```text
200 OK
```

with the complete `CANCELLED` job.

Cancelling an already cancelled job is idempotent and also returns:

```text
200 OK
```

with the existing job.

Cancelling:

```text
COMPLETED
FAILED
```

returns:

```text
409 Conflict
```

Unknown job:

```text
404 Not Found
```

---

# 25. Cancellation Race Semantics

Cancellation competes atomically with worker completion or failure.

For a running job, exactly one transition may win:

```text
RUNNING -> CANCELLED
RUNNING -> COMPLETED
RUNNING -> FAILED
```

If cancellation wins:

* the cancel endpoint returns `200`;
* final persisted state is `CANCELLED`;
* later worker completion or failure must not overwrite it;
* the running execution must receive a cooperative cancellation signal.

If completion or failure wins first:

* the cancellation request returns `409`;
* the existing `COMPLETED` or `FAILED` state remains unchanged.

Therefore:

> A successful cancellation response always guarantees a final `CANCELLED` state.

---

# 26. Running Cancellation Coordination

Running jobs must have an in-process cooperative cancellation mechanism.

The implementation must correctly handle races between:

* persisting `RUNNING`;
* registering the running execution;
* receiving a cancellation request;
* beginning actual job execution.

There must not be a window in which:

1. the API successfully persists `CANCELLED`;
2. the worker subsequently begins or continues meaningful execution because it missed the cancellation.

The exact synchronization design is implementation-specific.

---

# 27. Retry Job

Endpoint:

```text
POST /jobs/{id}/retry
```

Retry is valid only for:

```text
FAILED
```

and only when:

```text
attempt_count < 3
```

Successful retry atomically performs:

```text
FAILED -> QUEUED
```

and returns:

```text
200 OK
```

with the queued job snapshot.

On retry:

```text
queued_at = now
started_at = null
finished_at = null
result = null
error = null
```

`attempt_count` remains unchanged.

Retrying any other state returns:

```text
409 Conflict
```

Retrying a failed job with:

```text
attempt_count = 3
```

returns:

```text
409 Conflict
```

Unknown job:

```text
404 Not Found
```

---

# 28. Concurrent Retry Semantics

Retry is not idempotent.

If multiple retry requests race against the same failed job:

* exactly one may successfully perform `FAILED -> QUEUED`;
* that request returns `200`;
* remaining requests return `409`.

The race must not cause duplicate execution attempts or multiple queue entries.

---

# 29. Health Endpoint

Endpoint:

```text
GET /health
```

A healthy response requires:

1. the HTTP process is functioning;
2. the SQLite persistence layer successfully responds to a simple database operation.

Success:

```text
200 OK
```

Body:

```json
{
  "status": "ok"
}
```

If persistence cannot be accessed:

```text
503 Service Unavailable
```

using the standard API error representation with:

```text
code = PERSISTENCE_UNAVAILABLE
```

Do not expose raw SQLite errors to the client.

---

# 30. API Error Representation

All API errors must use:

```json
{
  "error": {
    "code": "ERROR_CODE",
    "message": "human-readable message"
  }
}
```

Required stable error codes:

```text
INVALID_JSON
INVALID_JOB_TYPE
INVALID_PAYLOAD
INVALID_FILTER
UNSUPPORTED_MEDIA_TYPE
JOB_NOT_FOUND
INVALID_STATE_TRANSITION
ATTEMPT_LIMIT_REACHED
METHOD_NOT_ALLOWED
ROUTE_NOT_FOUND
PERSISTENCE_UNAVAILABLE
INTERNAL_ERROR
```

Use:

```text
400
```

for invalid JSON, job type, payload, or filter.

Use:

```text
404
```

for unknown jobs and routes.

Use:

```text
405
```

for unsupported methods on known routes.

Use:

```text
409
```

for invalid state transitions or attempt-limit violations.

Use:

```text
415
```

for unsupported request media type.

Use:

```text
503
```

for health-check persistence failure.

Unexpected internal errors return:

```text
500 Internal Server Error
```

with:

```text
code = INTERNAL_ERROR
```

Never expose:

* stack traces;
* SQL statements;
* database paths;
* raw database errors.

---

# 31. HTTP Method Rules

Known routes must return:

```text
405 Method Not Allowed
```

for unsupported methods.

The response should include an appropriate:

```text
Allow
```

header identifying supported methods for that route.

Undefined routes return:

```text
404 Not Found
```

with:

```text
code = ROUTE_NOT_FOUND
```

---

# 32. Worker Pool

Start exactly:

```text
WORKER_COUNT
```

worker goroutines.

Workers continuously process queued jobs while the service is running.

Workers must:

* safely compete for jobs;
* survive individual job failures;
* stop claiming jobs when shutdown begins;
* execute independent jobs concurrently.

The implementation must not create an unbounded number of worker goroutines.

---

# 33. Persistence Guarantees

Every externally visible state change must be persisted before a successful API response is returned.

This includes:

```text
job creation
cancellation
retry
```

Every worker state transition must also be persisted before it is treated internally as complete.

Jobs must survive process restart.

A successful API mutation must never be acknowledged solely from in-memory state.

---

# 34. Startup Recovery

Recovery must execute:

1. after the database is initialized;
2. before the HTTP server begins accepting requests;
3. before workers begin claiming jobs.

Every persisted job in:

```text
RUNNING
```

must atomically become:

```text
FAILED
```

with:

```json
{
  "code": "INTERRUPTED_EXECUTION",
  "message": "job execution was interrupted by server termination"
}
```

Recovery sets:

```text
finished_at = recovery time
updated_at = recovery time
```

It preserves:

```text
attempt_count
created_at
queued_at
started_at
```

`result` must be `null`.

The interrupted execution already counted as an attempt.

If:

```text
attempt_count < 3
```

the recovered job may subsequently be retried.

---

# 35. Graceful Shutdown

Handle:

```text
SIGINT
SIGTERM
```

On graceful shutdown:

1. stop workers from claiming new jobs;
2. stop accepting new HTTP requests using `http.Server.Shutdown`;
3. signal cooperative cancellation to currently executing jobs;
4. transition affected `RUNNING` jobs to `FAILED` where that state is still current;
5. wait for workers to terminate;
6. close the database;
7. terminate.

Running jobs interrupted specifically by graceful server shutdown must use:

```json
{
  "code": "SERVER_SHUTDOWN",
  "message": "job execution was interrupted by server shutdown"
}
```

Queued jobs remain:

```text
QUEUED
```

If a user cancellation, normal completion, or normal failure already won the atomic state transition, shutdown must not overwrite that state.

The entire graceful shutdown process has a maximum duration of:

```text
10 seconds
```

If the timeout expires, the process may terminate.

Any job still persisted as `RUNNING` will be handled by startup recovery on the next start.

---

# 36. Logging

Use:

```text
log/slog
```

At minimum log:

```text
server started
server shutting down
startup recovery completed
job created
job claimed
job started
job completed
job failed
job cancelled
job retried
```

Job-related entries must include:

```text
job_id
job_type
```

Where relevant, also include:

```text
attempt_count
```

Do not log arbitrary complete job payloads.

Exact log formatting is not part of API compatibility.

---

# 37. Unit Tests

Unit tests must cover at least:

* valid state transitions;
* invalid state transitions;
* attempt-limit rules;
* hash execution;
* delay execution;
* intentional failure;
* job-input validation;
* retry state reset behavior;
* cancellation behavior.

---

# 38. API Integration Tests

Tests must cover every API endpoint.

At minimum verify:

### Create

* valid hash job;
* valid delay job;
* valid fail job;
* malformed JSON;
* unknown fields;
* unknown job type;
* invalid delay duration;
* unsupported media type.

### Get

* existing job;
* unknown job.

### List

* no filters;
* status filter;
* type filter;
* combined filters;
* invalid status;
* invalid type;
* deterministic ordering.

### Cancel

* queued job;
* running job;
* already cancelled job;
* completed job;
* failed job;
* unknown job.

### Retry

* failed job below attempt limit;
* failed job at attempt limit;
* non-failed job;
* unknown job.

### Health

* persistence available;
* persistence unavailable.

### Routing

* unsupported methods;
* unknown routes.

Tests must not require manually starting the server.

---

# 39. Persistence Tests

Verify that:

* newly created jobs survive database reopen;
* state changes survive database reopen;
* result data survives database reopen;
* error data survives database reopen;
* attempt counts survive database reopen;
* timestamps survive database reopen;
* retry correctly clears attempt-specific fields.

Use isolated temporary databases.

Tests must not share persistent mutable state unintentionally.

---

# 40. Concurrency Tests

The automated test suite must verify the following.

## Duplicate Execution

Multiple workers competing for the same queued job must result in exactly one execution attempt.

## Parallel Execution

Multiple independent delay jobs must demonstrably execute concurrently when `WORKER_COUNT > 1`.

## Concurrent Cancellation

Multiple simultaneous cancellation requests must produce:

```text
CANCELLED
```

with no invalid transition.

## Cancellation vs Completion

Force cancellation and normal completion to race.

Exactly one final state is permitted:

```text
CANCELLED
COMPLETED
```

The API response must agree with whichever atomic transition won.

## Cancellation vs Failure

Force cancellation and failure to race.

Exactly one final state is permitted:

```text
CANCELLED
FAILED
```

## Concurrent Retry

Multiple retry requests against one failed job must result in exactly one successful retry transition.

## Attempt Count

Concurrent worker activity must never increment one execution attempt more than once.

---

# 41. Recovery Tests

Create a persisted job in:

```text
RUNNING
```

with a non-zero `attempt_count`.

Restart or invoke startup recovery.

Verify:

```text
status = FAILED
error.code = INTERRUPTED_EXECUTION
attempt_count is unchanged
result = null
finished_at is populated
```

---

# 42. Shutdown Tests

At minimum verify that controlled service shutdown:

* stops additional job claiming;
* interrupts a running delay job;
* records `SERVER_SHUTDOWN` when shutdown wins the state transition;
* leaves queued jobs queued;
* terminates within a finite timeout.

Tests do not need to send real operating-system signals if the shutdown behavior can be invoked deterministically through application code.

---

# 43. Race Detection

The complete test suite must pass:

```bash
go test -race ./...
```

Any race-detector finding is a failure.

Do not:

* suppress race findings;
* skip race-sensitive tests;
* serialize tests solely to hide a genuine application race.

---

# 44. Required Verification

The final implementation must pass:

```bash
go build ./...
go test ./...
go test -race ./...
go vet ./...
```

Formatting verification:

```bash
gofmt -l .
```

must produce no output.

A non-empty `gofmt -l .` result means verification failed.

No required check may be:

* skipped;
* disabled;
* weakened;
* ignored.

No failing test may be removed or changed merely to satisfy completion.

No static-analysis finding may be suppressed solely to satisfy completion.

No coverage percentage threshold is required for version 1.

---

# 45. Human Auditability

Prefer conventional, explicit Go code.

Favor:

* straightforward control flow;
* small functions;
* clear package ownership;
* explicit error handling;
* conventional synchronization primitives;
* `context.Context` for cancellation;
* obvious state-transition logic.

Avoid unnecessary:

* reflection;
* generics;
* interface hierarchies;
* dependency injection;
* metaprogramming;
* custom frameworks;
* excessive layering;
* clever concurrency constructs;
* abstractions whose primary purpose is reducing line count.

Interfaces should be introduced only when they provide a concrete architectural or testing benefit.

Non-obvious concurrency invariants must be documented near the relevant implementation.

---

# 46. Project Structure

Use the smallest reasonable project structure.

A structure similar to this is acceptable:

```text
go.mod
go.sum
main.go

internal/
    api/
    jobs/
    store/
    worker/

README.md
```

This structure is not mandatory.

Use fewer packages if doing so produces a clearer implementation.

Do not create layers simply to match the example.

---

# 47. Dependencies

Required external dependency:

```text
modernc.org/sqlite
```

Additional direct dependencies should not be necessary.

If another dependency is introduced:

1. it must solve a concrete requirement;
2. the standard library must not already provide a reasonable solution;
3. its purpose must be documented in the README.

Do not introduce:

* HTTP frameworks;
* ORM libraries;
* dependency-injection frameworks;
* logging frameworks;
* configuration libraries

unless an explicit specification requirement cannot reasonably be satisfied without one.

---

# 48. README

Provide a `README.md` containing:

1. project purpose;
2. architecture overview;
3. prerequisites;
4. configuration;
5. build instructions;
6. run instructions;
7. test instructions;
8. race-detector instructions;
9. static-analysis instructions;
10. formatting verification;
11. API endpoint summary;
12. example API requests;
13. persistence behavior;
14. worker behavior;
15. cancellation semantics;
16. retry semantics;
17. startup recovery behavior;
18. graceful shutdown behavior;
19. dependency list and rationale;
20. known limitations.

Include a Mermaid or ASCII architecture diagram.

---

# 49. Definition of Done

The task is complete only when all of the following are true:

```text
✓ go build ./... succeeds

✓ go test ./... succeeds

✓ go test -race ./... succeeds

✓ go vet ./... succeeds

✓ gofmt -l . produces no output

✓ the REST API matches the specified contract

✓ JSON validation matches the specified rules

✓ all three job types behave as specified

✓ job state transitions are atomic

✓ multiple jobs can execute concurrently

✓ one job cannot execute concurrently more than once

✓ worker claiming is concurrency-safe

✓ attempt_count is concurrency-safe

✓ worker queue ordering is deterministic

✓ cancellation is idempotent

✓ successful cancellation guarantees final CANCELLED state

✓ cancellation/completion races behave as specified

✓ cancellation/failure races behave as specified

✓ running delay jobs respond promptly to cancellation

✓ retries are concurrency-safe

✓ maximum attempt count is enforced

✓ retry resets the specified execution fields

✓ jobs survive database restart

✓ startup recovery behaves as specified

✓ graceful shutdown behaves as specified

✓ no race-detector findings remain

✓ README documents operation and verification
```

No required verification check may be skipped or weakened.

---

# 50. Explicitly Out of Scope

Do not implement:

* CLI client
* web UI
* authentication
* authorization
* TLS termination
* distributed workers
* message brokers
* Redis
* PostgreSQL
* Kubernetes
* cloud deployment
* Docker configuration
* WebSockets
* automatic retries
* scheduled jobs
* job priorities
* starvation prevention
* multi-tenancy
* file uploads
* arbitrary subprocess execution
* Prometheus instrumentation
* distributed tracing
* API pagination
* historical storage of individual execution attempts
* database migration tooling beyond initial schema creation

Do not implement out-of-scope capabilities unless an explicit requirement in this specification cannot otherwise be satisfied.

---

# 51. Implementation Instruction

Plan and implement the complete system described in this specification according to the preconfigured planning and bounded-agent-execution procedures.

The specification is authoritative.

Do not request clarification for implementation choices explicitly delegated to the implementer.

Before implementation, briefly record:

1. proposed package structure;
2. persistence approach;
3. atomic state-transition strategy;
4. worker-claiming strategy;
5. running-job cancellation coordination strategy.

Then implement the system.

Continuously run deterministic verification during implementation rather than waiting until the end.

Use, as appropriate:

```bash
go test ./...
go test -race ./...
go vet ./...
go build ./...
gofmt -l .
```

Do not weaken:

* tests;
* race detection;
* state invariants;
* validation;
* persistence guarantees;
* concurrency guarantees

to reach completion.

Do not add improvements, abstractions, or features after all specification requirements and deterministic completion checks pass.

When the Definition of Done is satisfied, stop execution.

At completion, report:

1. architecture summary;
2. major implementation decisions;
3. verification commands executed;
4. test results;
5. race-detector results;
6. static-analysis results;
7. known limitations;
8. any requirement that could not be fully satisfied.
