# TaskForge — Independent Code Quality Analysis

Static analysis of the `experiment/go-job-processing-service` arm, run **after**
the arm completed, by tools that had no part in producing the code.

**Why this file exists.** The experiment's premise is that a passing test suite
is necessary but not sufficient evidence of quality. The obvious follow-up
question is whether the tests pass because the code is good, or because the same
process wrote both. Independent static analysis is a checkable answer to that,
where "an experienced Go developer skimmed it and it looked fine" is not.

Run at commit `47fc70e`, on `2,473` production lines across 23 files (`7,305`
lines total including tests). Every tool was installed into a **throwaway
container**, never into the pinned sandbox image, so the apparatus is unchanged.

---

## 1. Summary

| Check | Result | Verdict |
|---|---|---|
| `golangci-lint` (default set) | 5 findings, all `errcheck` | clean |
| `golangci-lint` (13-linter extended set) | 9 findings total | clean |
| `gosec` | 8 findings: 6 LOW, 2 MEDIUM | 1 real defect |
| Test coverage | **83.0 %** of statements | strong |
| Cyclomatic complexity | 0 production functions > 12 | strong |
| `go vet` / `gofmt` | clean | clean |
| Race detector | clean | clean |

**Overall: good.** Nine findings across 7,305 lines, of which **one is a genuine
defect worth fixing** and the rest are either intentional, idiomatic, or false
positives. No finding indicates a correctness or concurrency problem, which is
notable given the concurrency-heavy design.

---

## 2. The one real defect

### G112 — `http.Server` has no timeouts (`app.go:105`)

```go
server := &http.Server{Handler: handler.Routes()}
```

No `ReadHeaderTimeout`, `ReadTimeout`, `WriteTimeout` or `IdleTimeout`. A client
that opens a connection and sends headers slowly holds a goroutine indefinitely
— the Slowloris pattern. gosec rates it MEDIUM severity, HIGH confidence.

**This is a real, if minor, production concern**, and the fix is one line
(`ReadHeaderTimeout: 10 * time.Second`).

**Was it in scope?** The specification does not mention server timeouts, and
§9-style "do not add what is not required" constraints applied throughout the
run. So this is not a violation of the brief — it is the kind of hardening a
human reviewer would flag that neither the spec nor the tests asked for.

That distinction is the interesting part for the experiment: the agents built
exactly what was specified, and this is a gap in the *specification*, not in the
execution of it.

---

## 3. Findings judged not to be defects

Each was inspected rather than dismissed by rule id.

### G202 — "SQL string concatenation" (`internal/store/list.go:45`) — **false positive**

```go
query := "SELECT " + jobColumns + " FROM jobs"
if len(conditions) > 0 {
    query += " WHERE " + strings.Join(conditions, " AND ")
}
```

Reads as SQL injection, and is not. **No user-controlled input is concatenated
at all** — the concatenation assembles the query *template*, and the values
travel separately:

```go
conditions = append(conditions, "status = ?")   // template fragment: a string literal
args       = append(args, string(*filter.Status)) // the value: never touches the SQL text
...
rows, err := s.db.QueryContext(ctx, query, args...)
```

Every fragment joined is a compile-time literal, so the finished statement is
always one of exactly four strings — the base query, plus optionally
`WHERE status = ?`, `WHERE type = ?`, or both. User values reach the database
only as bound parameters.

The safety here comes from **parameterization**, not from sanitization. Worth
being precise about, because the two are often conflated: `Valid()` is called on
the filter before its fragment is appended, but that check is about rejecting a
nonsense filter with `ErrInvalidFilter` rather than silently matching nothing —
it is not what prevents injection, and removing it would not create an injection
vector. Conversely, no amount of validation would make string-interpolated values
safe.

(Strictly, `QueryContext` with arguments performs *parameter binding*; whether
the driver also prepares and caches the statement is an implementation detail.
The security property depends on the values being transmitted separately from
the SQL text, not on statement preparation.)

The true-positive shape of this rule would be a value interpolated into a
fragment — `"status = '" + string(*filter.Status) + "'"`. The code does not do
that anywhere.

Worth stating explicitly because the store layer is hand-written SQL throughout,
and this was the single highest-value thing for an independent tool to check.

### `nilerr` — returns nil despite non-nil error (`internal/worker/pool.go:197`)

```go
case err != nil:
    if p.claimCtx.Err() != nil {
        return false, nil // shutting down; not a failure
    }
    return false, err
```

Intentional and correct: a claim failing *because the pool is shutting down* is
not an error condition. The comment says so, and the surrounding switch handles
the genuine error case one line below.

### `exitAfterDefer` — `os.Exit` skips `defer cancel()` (`main.go:44`)

Technically true; irrelevant in practice. The `defer cancel()` releases a context
at the moment the process exits, so nothing leaks that outlives it. Idiomatic in
`main`.

### G104 / `errcheck` — unchecked errors (6 findings)

| Location | Call | Assessment |
|---|---|---|
| `internal/api/errors.go:57,62` | `w.Write(...)` | Writing an error response; nothing useful to do if it fails. Idiomatic. |
| `app.go:65,88` | `st.Close()`, `listener.Close()` | Cleanup on an already-failing construction path. |
| `app_test.go:152`, `main_binary_test.go:73,88` | `Shutdown`, `Process.Kill` | Test teardown. |

None is a correctness issue. A stricter house style would check or explicitly
discard them; the code is consistent about which it ignores.

### `unparam` — unused parameter in two table-test closures

`internal/api/retry_test.go` — two of five cases in a table-driven test do not
use the `*store.Store` argument their shared signature provides. Normal for
table tests.

---

## 4. Coverage

`go test -coverprofile -covermode=atomic ./...`

| Package | Coverage |
|---|---:|
| `internal/jobs` | 92.7 % |
| `internal/worker` | 89.8 % |
| `internal/api` | 84.1 % |
| `internal/store` | 79.5 % |
| `.` (composition root) | 68.3 % |
| **Total** | **83.0 %** |

83 % overall with the highest coverage on the domain and concurrency packages —
the places where defects would be most costly — and the lowest on the composition
root, where much of the uncovered code is signal handling and error paths that
are awkward to exercise in-process.

The distribution is what a careful engineer would aim for, not a uniform number
chased for its own sake.

---

## 5. Complexity

`gocyclo`, whole repository:

| Rank | Complexity | Function | Kind |
|---:|---:|---|---|
| 1 | 21 | `TestRecover` | test |
| 2 | 19 | `TestCreateGet_RoundTripsAllFields` | test |
| 3 | 13 | `TestBinaryStartsWithDefaults…` | test |
| 4 | 13 | `TestRetry_ConcurrentExactlyOneWinner` | test |
| 5 | 13 | `TestCreateGet_RoundTripsNonNilOptionalFields` | test |
| 6 | 13 | `TestRace_CancellationVsFailure` | test |
| 7 | 13 | `TestRace_CancellationVsCompletion` | test |
| 8 | 12 | `TestCancelBetweenClaimAndRegistration…` | test |

**Only 2 functions in the repository exceed complexity 15, and both are tests.**
No production function exceeds 12.

That the most complex functions are all concurrency and round-trip *tests* is a
good sign rather than a bad one — the complexity sits where behaviour is being
exercised, not where it is implemented.

---

## 6. What this does and does not establish

**Establishes:** the code is conventionally sound. No injection vector, no
unchecked error that matters, no complexity hotspot, no dead code, no obviously
wrong idiom, and coverage concentrated where risk is.

**Does not establish:** that the *design* is right. No linter can tell you
whether the claim-then-register ordering closes the SPEC §26 race, or whether the
transition table is complete. The strongest evidence for those remains the
mutation tests recorded in `EXECUTION_NOTES.md` — where disabling the
`confirmRunning` guard made a dedicated test fail deterministically while the
timing-based stress test stayed green.

Static analysis complements that evidence. It does not replace it.

**One honest caveat:** these tools were run by the same session that orchestrated
the run. The tool *outputs* are reproducible by anyone — the commands are below —
but the judgment calls in §3 about which findings are false positives are mine,
and a reviewer should check them rather than take them on trust. The raw output
is preserved so that is possible.

---

## 7. Reproducing this

Inside the sandbox image at commit `47fc70e`:

```bash
go install github.com/golangci/golangci-lint/cmd/golangci-lint@v1.62.2
go install github.com/securego/gosec/v2/cmd/gosec@latest
go install github.com/fzipp/gocyclo/cmd/gocyclo@latest

golangci-lint run ./...
golangci-lint run --no-config -E staticcheck,govet,errcheck,ineffassign,\
unconvert,unparam,bodyclose,gocritic,misspell,gosimple,nilerr,prealloc,copyloopvar ./...
gosec ./...
go test -coverprofile=cov.out -covermode=atomic ./... && go tool cover -func=cov.out | tail -1
gocyclo -top 10 .
```

Versions: `golangci-lint v1.62.2`, `gosec` (latest at run time), Go `1.23.5`,
`linux/arm64`.
