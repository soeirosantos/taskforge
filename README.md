# taskforge-health

A minimal HTTP health-check service in Rust. It exposes a single endpoint,
`GET /health`, which reports that the service is up.

## Prerequisites

* A stable Rust toolchain with Cargo (verified on Rust 1.83).
* The `rustfmt` and `clippy` components, required for static verification:

```bash
rustup component add rustfmt clippy
```

## Running

```bash
cargo run
```

The service prints `server starting on port 8080` and listens on `0.0.0.0:8080`.

## Tests

```bash
cargo test
```

## Static verification

```bash
cargo build
cargo test
cargo fmt --all -- --check
cargo clippy --all-targets --all-features -- -D warnings
```

## Calling the health endpoint

```bash
curl http://localhost:8080/health
```

Returns HTTP 200 with `Content-Type: application/json` and the body:

```json
{"status":"ok"}
```

`HEAD /health` is served by the same handler. Any other method on `/health`
returns 405, and unknown paths return 404.

## Overriding the port

```bash
PORT=9000 cargo run
```

If `PORT` is unset, the service uses 8080. Otherwise the value must parse as an
integer between 1 and 65535. Non-numeric values (`abc`), out-of-range values
(`70000`), `0`, and empty or whitespace-only values are rejected: the service
writes a message to stderr, for example

```text
invalid PORT value "abc": expected a number between 1 and 65535
```

and exits with status 1. It never silently falls back to 8080.
