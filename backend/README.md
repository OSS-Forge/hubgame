# HubGame Backend

This backend now supports both:
- `Monolith mode` (`backend/cmd/server`) for local development
- `Split services` (`gateway`, `controller`, `db-engine`) for containerized deployment

## Implemented Backbone
- SQLite-backed custom entity/event store in Go
- Embedded DB controllers (schema validation + tenant guard hooks)
- Optimistic concurrency via `If-Match` / expected version
- Native websocket topic streaming
- JWT auth controller service
- RBAC action matrix at gateway
- Gateway-to-db-engine internal service auth

## Service Split
- `gateway` (public): auth verify, RBAC enforcement, request forwarding, websocket proxy
- `controller` (internal/public auth): token issue + verify
- `db-engine` (internal): storage, event log, schema-enforced write pipeline

## Run (Monolith)
```bash
go run ./backend/cmd/server
```

## Run (Split via Docker Compose)
```bash
docker compose up --build
```

Public entrypoint: `http://localhost:8080`

## Token Issuance (Split Mode)
Issue token from controller:

```bash
curl -X POST http://localhost:8082/v1/auth/token \
  -H 'Content-Type: application/json' \
  -H 'X-Controller-Admin: dev-controller-admin' \
  -d '{"user_id":"u1","tenant_id":"t1","role":"developer","ttl_seconds":3600}'
```

Use returned token on gateway endpoints:

```bash
curl http://localhost:8080/v1/entities?kind=match \
  -H "Authorization: Bearer <TOKEN>"
```

## Endpoints
Gateway (`:8080`):
- `GET /healthz`
- `GET|POST /v1/entities`
- `GET|PATCH|DELETE /v1/entities/{id}`
- `GET|POST /v1/events`
- `GET /v1/events/stream?topic=entity.user` (WebSocket)

Controller (`:8082`):
- `GET /healthz`
- `POST /v1/auth/token`
- `POST /v1/auth/verify`
