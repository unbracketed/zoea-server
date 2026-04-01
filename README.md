# Go Agent Gateway (Scaffold)

Scaffold implementation for a gateway that bridges HTTP/WebSocket clients to `pi --mode rpc` subprocesses.

## Current state

This scaffold includes:
- project layout
- config wiring
- HTTP API skeleton
- in-memory session manager
- **real Pi RPC subprocess manager** (`pi --mode rpc`)
- JSONL RPC reader/writer with command `id` correlation
- websocket stream endpoint for raw agent events

Still TODO:
- persistence backend
- JWT auth / per-user ACL middleware
- normalized event mapping (currently forwards raw event payload)

## Run

```bash
cd go-agent-gateway
go run ./cmd/gateway
```

Server default: `:8080`

## Endpoints (scaffold)

- `POST /v1/sessions`
- `GET /v1/sessions/{id}/state`
- `GET /v1/sessions/{id}/messages`
- `POST /v1/sessions/{id}/messages`
- `POST /v1/sessions/{id}/abort`
- `GET /v1/sessions/{id}/stream` (WebSocket)
- `DELETE /v1/sessions/{id}`
- `GET /healthz`
