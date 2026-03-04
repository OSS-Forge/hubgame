# HubGame Backend

This backend supports:
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
- Browser-ready CORS middleware for web integration

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

## Debug Logging
Enable verbose backend traces with:
```bash
HUBGAME_DEBUG=true
```

When enabled, all services log:
- inbound request method/path/status/latency
- gateway proxy upstream calls and statuses
- Tik-Toe matchmaking queue/match decisions

Tail logs while testing matchmaking:
```bash
docker compose logs -f gateway db-engine controller
```

## Seed Catalog (Versioned)
Local Go command:
```bash
go run ./backend/cmd/seed-catalog
```

Re-apply same version explicitly:
```bash
go run ./backend/cmd/seed-catalog -- -force
```

Containerized one-off seed:
```bash
docker compose run --rm --profile tools seed-catalog
```

## Dev Token Endpoint (Gateway)
When `HUBGAME_ENABLE_DEV_AUTH=true`, gateway exposes:
- `POST /v1/auth/dev-token`

This endpoint obtains a JWT from controller using internal admin credentials and is intended for local/dev only.

## Token Issuance (Controller Direct)
```bash
curl -X POST http://localhost:8082/v1/auth/token \
  -H 'Content-Type: application/json' \
  -H 'X-Controller-Admin: dev-controller-admin' \
  -d '{"user_id":"u1","tenant_id":"t1","role":"developer","ttl_seconds":3600}'
```

Use returned token on gateway endpoints:
```bash
curl http://localhost:8080/v1/entities?kind=game \
  -H "Authorization: Bearer <TOKEN>"
```

## Endpoints
Gateway (`:8080`):
- `GET /healthz`
- `POST /v1/auth/dev-token` (dev mode)
- `GET|POST /v1/entities`
- `GET|PATCH|DELETE /v1/entities/{id}`
- `GET|POST /v1/events`
- `GET /v1/events/stream?topic=entity.game`
- `GET /v1/leaderboard?scope=global|game&game_id=<id>&limit=<n>`
- `POST /v1/leaderboard/users`
- `POST /v1/leaderboard/scores`
- `POST /v1/tiktoe/matches` (create offline/bot/online match state)
- `GET /v1/tiktoe/matches/{match_id}`
- `POST /v1/tiktoe/matches/{match_id}/moves`
- `GET|POST /v1/tiktoe/matches/{match_id}/chat`
- `POST /v1/tiktoe/matchmaking/enqueue`
- `GET /v1/tiktoe/matchmaking/status`

Direct username match:
- `POST /v1/tiktoe/matches` with:
  - `mode: "online"`
  - `player_id`
  - `opponent_id`
This creates (or returns) a deterministic direct match between the two usernames.

Controller (`:8082`):
- `GET /healthz`
- `POST /v1/auth/token`
- `POST /v1/auth/verify`

## Leaderboards and Hubcoins
Developers can integrate leaderboard workflows through gateway:

Create/update a user profile:
```bash
curl -X POST http://localhost:8080/v1/leaderboard/users \
  -H "Authorization: Bearer <TOKEN>" \
  -H "Content-Type: application/json" \
  -d '{"user_id":"u1","display_name":"Ada","rank_title":"Gold","hubcoins":120}'
```

Submit score and hubcoin rewards for a game:
```bash
curl -X POST http://localhost:8080/v1/leaderboard/scores \
  -H "Authorization: Bearer <TOKEN>" \
  -H "Content-Type: application/json" \
  -d '{"game_id":"tik-toe","user_id":"u1","score_delta":25,"hubcoins_delta":8}'
```

Read global leaderboard:
```bash
curl "http://localhost:8080/v1/leaderboard?scope=global&limit=20" \
  -H "Authorization: Bearer <TOKEN>"
```

Read per-game leaderboard:
```bash
curl "http://localhost:8080/v1/leaderboard?scope=game&game_id=tik-toe&limit=20" \
  -H "Authorization: Bearer <TOKEN>"
```

Policy note:
- `hubcoins` are virtual in-platform credits.
- `hubcoins` are not purchasable with real-world money.
- `hubcoins` cannot be converted, exchanged, or redeemed for real-world money.
