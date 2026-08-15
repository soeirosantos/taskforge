# Go Health Check Service

A minimal HTTP service in Go exposing a health-check endpoint. Built as a smoke test for an automated coding-agent workflow.

## Running the Service

Start the server with:

```bash
go run .
```

The service listens on port `8080` by default. To override the port, set the `PORT` environment variable:

```bash
PORT=9090 go run .
```

## Running Tests

Run the automated test suite:

```bash
go test ./...
```

## Verification

To verify the implementation, run:

```bash
go test ./...
go vet ./...
gofmt -l .
go build ./...
```

All checks must pass with no errors. Source files are expected to be correctly formatted; use `gofmt -w .` if needed.

## Health Endpoint

The service exposes a single health-check endpoint:

### GET /health

Returns HTTP 200 with JSON response:

```bash
curl http://localhost:8080/health
```

Expected response:

```json
{
  "status": "ok"
}
```

Response header: `Content-Type: application/json`

### Other Methods and Routes

- `POST /health` returns HTTP 405 (Method Not Allowed)
- Unknown routes return HTTP 404 (Not Found)
