# HubGame Backend (Foundation)

First implementation of the backend backbone:
- SQLite-backed custom store in Go
- Embedded controller hooks in DB operations
- Event store + in-memory pub/sub broker
- HTTP CRUD API for entities/events
- Native WebSocket event stream endpoint
- JWT auth controller and tenant guard

## Run

```bash
go run ./backend/cmd/server
```

## Quick token generation (manual example)
Use your own app flow to issue token with `AuthController.IssueToken(...)`.
Then call endpoints with `Authorization: Bearer <token>`.

## Endpoints
- `GET /healthz`
- `GET|POST /v1/entities`
- `GET|PATCH|DELETE /v1/entities/{id}`
- `GET|POST /v1/events`
- `GET /v1/events/stream?topic=entity.user` (WebSocket)
