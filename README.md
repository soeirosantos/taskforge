# TaskForge

TaskForge is an asynchronous job-processing service. It exposes a REST API for
submitting and managing jobs, persists every job and state transition to
SQLite, and executes queued jobs with a bounded pool of worker goroutines.

Three job types are supported:

| Type    | Behaviour                                                    |
|---------|---------------------------------------------------------------|
| `hash`  | Computes the SHA-256 digest of a UTF-8 string.                 |
| `delay` | Waits 100–30000 ms, cooperatively cancellable mid-wait.        |
| `fail`  | Always fails. Exists to exercise the retry path.               |

## Architecture

```
                      ┌─────────────────────────┐
                      │        main.go          │
                      │  config.go / app.go      │
                      │  (composition root)      │
                      └────────────┬─────────────┘
                                   │ wires
              ┌────────────────────┼────────────────────┐
              │                    │                     │
              ▼                    ▼                     ▼
      ┌───────────────┐   ┌────────────────┐   ┌──────────────────┐
      │  internal/api  │   │ internal/worker │   │  internal/jobs    │
      │  HTTP layer    │   │  worker pool    │   │  domain types,    │
      │  routing,      │   │  (N goroutines) │   │  validation,      │
      │  handlers,     │   │                 │   │  executors        │
      │  JSON errors   │   │  ┌────────────┐ │   └──────────────────┘
      └───────┬───────┘   │  │ cancellation│ │
              │           │  │  registry   │◄┼──────┐  shared *Registry
              │           │  │ (in-memory) │ │      │  (SPEC 26)
              │◄──────────┼──┴────────────┘ │      │
              │  signals  └────────┬────────┘      │
              │  cancel via                        │
              │  the same registry                  │
              │                    │                 │
              ▼                    ▼                 │
      ┌─────────────────────────────────────────┐    │
      │           internal/store                 │◄───┘
      │  every state transition is a single       │
      │  guarded  UPDATE ... WHERE id=? AND       │
      │  status=?  (RowsAffected==1 wins)         │
      └────────────────────┬──────────────────────┘
                            │
                            ▼
                   ┌──────────────────┐
                   │  SQLite file      │
                   │  (DATABASE_PATH)  │
                   │  single writer    │
                   │  (SetMaxOpenConns │
                   │   (1))            │
                   └──────────────────┘
```

The HTTP layer and the worker pool never talk to each other directly except
through two shared components: `internal/store.Store` (the source of truth for
job state) and `internal/worker.Registry` (the in-process map of currently
running executions, used only to deliver a cancellation signal to a job that
is mid-flight; it is never consulted to decide whether a transition is
allowed — the store's guarded `UPDATE` is what decides that).

```
internal/
  jobs/    domain: Job, Status, Type, transitions, payload validation, executors
  store/   SQLite persistence and every atomic state transition
  worker/  worker pool and the running-job cancellation registry
  api/     HTTP routing, handlers, JSON error envelope
main.go, app.go, config.go   composition root
```

## Prerequisites

- Go 1.23.5 or later (the toolchain declared in `go.mod`).
- No CGO toolchain and no system SQLite are required — the SQLite driver
  (`modernc.org/sqlite`) is pure Go.
- No external services. The database is a single local file, created
  automatically on first run.

## Configuration

All configuration is read from the environment at startup. An invalid value
causes the process to exit immediately with a non-zero status and a message
naming the offending variable; nothing partially starts.

| Variable        | Default            | Valid range                          | Notes                                                              |
|------------------|--------------------|--------------------------------------|----------------------------------------------------------------------|
| `PORT`           | `8080`             | integer, 1–65535                     | HTTP listen port.                                                    |
| `WORKER_COUNT`   | `4`                | integer, 1–64                        | Number of worker goroutines, fixed for the process lifetime.         |
| `DATABASE_PATH`  | `./taskforge.db`   | non-empty string                     | Unset uses the default; **explicitly set to an empty string is invalid** — unset and empty are treated differently. |

The graceful shutdown timeout is a fixed 10 seconds and has no environment
variable.

## Build

```
go build ./...
```

## Run

```
go run .
```

Or, with configuration overrides:

```
PORT=9090 WORKER_COUNT=8 DATABASE_PATH=./data/taskforge.db go run .
```

The server logs `server started` once the HTTP listener is accepting
connections and the worker pool has started. Stop it with `Ctrl-C`
(`SIGINT`) or `SIGTERM` to trigger graceful shutdown.

## Test

```
go test ./...
```

## Race detector

```
go test -race ./...
```

## Static analysis

```
go vet ./...
```

## Formatting verification

```
gofmt -l .
```

This must produce no output. Any listed file is not gofmt-formatted.

## API

All responses are `application/json`. Errors use a uniform envelope (see
below).

| Method | Path                  | Description                                             |
|--------|------------------------|-----------------------------------------------------------|
| POST   | `/jobs`                | Create a job.                                              |
| GET    | `/jobs`                | List jobs, optionally filtered by `status` and/or `type`.  |
| GET    | `/jobs/{id}`           | Fetch a single job.                                        |
| POST   | `/jobs/{id}/cancel`    | Cancel a queued or running job.                             |
| POST   | `/jobs/{id}/retry`     | Re-queue a failed job.                                      |
| GET    | `/health`              | Liveness/readiness check, including database connectivity.  |

An unsupported method on a known route returns `405 METHOD_NOT_ALLOWED` with
an `Allow` header. An unknown route returns `404 ROUTE_NOT_FOUND`.

### Example requests

Create a `hash` job:

```
$ curl -s -X POST localhost:8080/jobs -H 'Content-Type: application/json' \
    -d '{"type":"hash","payload":{"text":"hello world"}}'
{"id":"909d2341-b806-4a65-b47a-c21c644f8f2d","type":"hash","status":"QUEUED",
 "payload":{"text":"hello world"},"result":null,"error":null,"attempt_count":0,
 "created_at":"2026-08-16T01:49:12.288361656Z","queued_at":"2026-08-16T01:49:12.288361656Z",
 "started_at":null,"finished_at":null,"updated_at":"2026-08-16T01:49:12.288361656Z"}
```

Once a worker has picked it up and it completes, `GET /jobs/{id}` returns:

```
{"id":"909d2341-b806-4a65-b47a-c21c644f8f2d","type":"hash","status":"COMPLETED",
 "payload":{"text":"hello world"},
 "result":{"sha256":"b94d27b9934d3e08a52e52d7da7dabfac484efe37a5380ee9088f7ace2efcde9"},
 "error":null,"attempt_count":1, ...}
```

List only failed jobs:

```
$ curl -s 'localhost:8080/jobs?status=FAILED'
```

List only queued `delay` jobs:

```
$ curl -s 'localhost:8080/jobs?status=QUEUED&type=delay'
```

Cancel a job:

```
$ curl -s -X POST localhost:8080/jobs/909d2341-b806-4a65-b47a-c21c644f8f2d/cancel
```

Retry a failed job:

```
$ curl -s -X POST localhost:8080/jobs/909d2341-b806-4a65-b47a-c21c644f8f2d/retry
```

### Error envelope

```
{"error":{"code":"INVALID_PAYLOAD","message":"payload field \"text\" is required"}}
```

Codes: `INVALID_JSON`, `INVALID_JOB_TYPE`, `INVALID_PAYLOAD`,
`INVALID_FILTER`, `UNSUPPORTED_MEDIA_TYPE`, `JOB_NOT_FOUND`,
`INVALID_STATE_TRANSITION`, `ATTEMPT_LIMIT_REACHED`, `METHOD_NOT_ALLOWED`,
`ROUTE_NOT_FOUND`, `PERSISTENCE_UNAVAILABLE`, `INTERNAL_ERROR`.

## Persistence

Jobs are stored in a single SQLite table, opened (and its schema created, if
missing) at startup — there is no separate migration step. The connection
pool is deliberately limited to one connection
(`db.SetMaxOpenConns(1)`), matching SQLite's single-writer model and making
write ordering deterministic at the cost of write throughput under
concurrency.

Every state transition — claim, complete, fail, cancel, retry — is a single
guarded statement of the form `UPDATE jobs SET ... WHERE id = ? AND status =
?`. Whichever caller observes `RowsAffected() == 1` won that transition;
every other concurrent caller observes 0 rows affected and reports a
conflict. There is no read-then-write on any transition path, which is what
makes competing transitions (e.g. a cancel racing a worker's completion)
safe without additional locking.

Timestamps are stored as `TEXT` in a fixed-width layout
(`2006-01-02T15:04:05.000000000Z07:00`, always UTC) rather than
`time.RFC3339Nano`. RFC3339Nano strips trailing zeros from the fractional
second, which would make `"...00.1Z"` sort after `"...00.15Z"` under plain
string comparison — breaking both the worker's claim order and `GET /jobs`
list order, both of which are SQL `ORDER BY` on these text columns.

## Worker behaviour

Exactly `WORKER_COUNT` goroutines are started once, for the life of the
process — there is no goroutine-per-job. Each worker loops: try to claim the
oldest queued job (a single guarded `UPDATE ... WHERE status = 'QUEUED' ...`
combined with the select, ordered by `queued_at, id`), and if none is
available, sleep for a short poll interval before trying again. Because the
claim selects and transitions in the same statement, a worker only learns a
job's id once it is already persisted as `RUNNING`.

After claiming, a worker registers the job's execution in the shared
cancellation registry, then re-reads the persisted row to confirm it is still
`RUNNING` before starting real work. That extra read (one indexed `SELECT`
per claimed job) closes the narrow window between the claim committing and
the registry entry existing, during which a cancellation could otherwise be
persisted and never observed. It is a deliberate cost, chosen for
auditability over a more intricate lock-free scheme.

A store failure while claiming does not kill the worker; it logs and retries
after the poll interval.

## Cancellation semantics

`POST /jobs/{id}/cancel` is valid from `QUEUED` and `RUNNING`:

- From `QUEUED` or `RUNNING`, on success the job becomes `CANCELLED` (200).
- If the job is already `CANCELLED`, the endpoint is idempotent and returns
  200.
- If the job is `COMPLETED` or `FAILED`, it returns 409
  `INVALID_STATE_TRANSITION`.

A successful cancellation wins an atomic `UPDATE ... WHERE status IN
('QUEUED','RUNNING')` before anything else happens, which guarantees
`CANCELLED` is final: a worker that is mid-execution and finishes afterwards
attempts its own guarded `RUNNING -> {COMPLETED,FAILED}` transition, which
loses (0 rows affected) and is silently discarded. `CANCELLED` can never be
overwritten.

Cancelling a running job also signals the in-memory cancellation registry, so
the executor observes it cooperatively (a `select` on the execution context)
rather than being forcibly killed. A running 5-second `delay` job was
observed to stop within 23 ms of being cancelled.

## Retry semantics

`POST /jobs/{id}/retry` is valid only from `FAILED`, and only while
`attempt_count < 3` (`MaxAttempts`). On success it re-queues the job:
`queued_at` is reset to now, `started_at`, `finished_at`, `result`, and
`error` are cleared, and `attempt_count` is left unchanged (it increments
only when a worker next claims the job, not on retry itself).

- Not `FAILED`: 409 `INVALID_STATE_TRANSITION`.
- `FAILED` but `attempt_count` already at 3: 409 `ATTEMPT_LIMIT_REACHED`.

Retry is not idempotent: the underlying transition is guarded on `status =
'FAILED'`, so of several concurrent retry requests against the same job,
exactly one observes the winning `UPDATE` and returns 200; the rest observe 0
rows affected and return 409.

## Startup recovery

Before the HTTP listener starts accepting connections and before any worker
claims a job, the store scans for every job persisted as `RUNNING` (which can
only mean the previous process died mid-execution) and transitions each to
`FAILED` with:

```
{"code":"INTERRUPTED_EXECUTION","message":"job execution was interrupted by server termination"}
```

`attempt_count`, `created_at`, `queued_at`, and `started_at` are preserved
unchanged; only `status`, `error`, `finished_at`, and `updated_at` change.
This runs synchronously in `newApp`, ahead of `Start`, so neither the server
nor a worker can ever observe a stale `RUNNING` row.

## Graceful shutdown

On `SIGINT` or `SIGTERM`, bounded by a fixed 10-second timeout:

1. Workers stop claiming new jobs (queued jobs are left `QUEUED`).
2. The HTTP server stops accepting new connections and drains in-flight
   requests.
3. Cooperative cancellation is signalled to every execution still in flight
   via the shared registry.
4. Each affected worker's own guarded `RUNNING -> FAILED` transition records:

   ```
   {"code":"SERVER_SHUTDOWN","message":"job execution was interrupted by server shutdown"}
   ```

   This is the same guarded transition used on normal completion — if the
   job had already reached a terminal state (e.g. it won a race and
   completed, or was independently cancelled) by the time this runs, the
   guarded update is a no-op and that terminal state is never overwritten.
5. Shutdown waits for all workers to return, bounded by the same 10-second
   context; if it times out first, shutdown proceeds anyway and any job
   still `RUNNING` is picked up by startup recovery on the next start.
6. The database connection is closed.

## Dependencies and rationale

`modernc.org/sqlite` is the **only** direct dependency, pinned to **v1.39.0**.
It is required for SQLite persistence and chosen specifically because it is
a pure-Go driver — no CGO, no system SQLite library needed to build or run.
It is pinned rather than left on latest because releases from v1.40 onward
declare `go 1.24`/`go 1.25` in their own `go.mod`, which would force a Go
toolchain upgrade this environment must not perform; v1.39.0 is the newest
release still compatible with the Go 1.23 toolchain used here.

Everything else is the standard library: `net/http` (routing and the HTTP
server), `database/sql` (the store's SQL access), `encoding/json` (the API
representation), `log/slog` (structured logging), `context` (cancellation and
deadlines throughout), and `crypto/rand` (job id generation).

Job ids are hand-rolled RFC 4122 UUIDv4 values built directly from
`crypto/rand`, rather than pulling in a UUID library, since generating
sixteen random bytes with the version/variant bits set does not warrant a
dependency.

### `EXECUTION_ERROR` — an addition beyond the specification

The job error code set required by the specification does not include an
error for "the executor returned something that is neither the job's own
failure nor a cancellation." TaskForge adds `EXECUTION_ERROR` to cover
exactly that defensive branch. Because payloads are validated before a job
is ever persisted, this branch is unreachable in normal operation — but
without it, such an error would leave the job stranded in `RUNNING` until
the next startup recovery instead of resolving immediately. It is documented
here explicitly as an addition beyond SPEC's error code list, not a
substitute for any of the specified codes.

## Known limitations

- A single database connection (`SetMaxOpenConns(1)`) bounds write
  throughput; this trades performance for deterministic write ordering.
- Workers poll for work on a fixed interval rather than being notified of
  new jobs, so there is a small (bounded by the poll interval) latency
  between a job being queued and being claimed.
- `GET /jobs` has no pagination; it returns the full matching result set.
- There is no automatic retry — a `FAILED` job stays `FAILED` until a client
  calls `POST /jobs/{id}/retry`.
- No authentication, authorization, TLS, or multi-tenancy.
- No distributed workers, message broker, metrics endpoint, job scheduling,
  or job priorities.
- Attempt history is not retained: only the current `attempt_count` is
  stored, not a record of each individual attempt.
