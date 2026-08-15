# Go Health Check Service

## Purpose

A minimal HTTP service exposing a health-check endpoint that returns the application's status as JSON.

## Running the Service

Run the service with:

```bash
go run .
```

The service listens on port `8080` by default. To use a different port, set the `PORT` environment variable:

```bash
PORT=9000 go run .
```

## Running Tests

Run the test suite with:

```bash
go test ./...
```

## Verification

Run the following commands to verify the implementation:

```bash
go build ./...
go test ./...
go vet ./...
gofmt -l .
```

## Health Endpoint

Once the service is running, call the health endpoint with:

```bash
curl http://localhost:8080/health
```

Expected response:

```json
{"status":"ok"}
```
