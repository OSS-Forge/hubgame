# HubGame Progress

## 2026-03-04
- Initialized Go module and backend foundation.
- Implemented custom SQLite-based store with CRUD + event log + pub/sub.
- Added JWT auth controller, tenant guard, and websocket streaming support.
- Added RBAC action matrix for gateway authorization.
- Added schema-validation DB controller and optimistic concurrency support (`If-Match`).
- Split backend into containerized services: `gateway`, `controller`, and `db-engine`.
- Added Dockerfile + `docker-compose.yml` for local orchestration.
- Added end-to-end integration test for auth -> gateway -> db-engine flow, including websocket streaming and version conflict checks.
- Added integration coverage for RBAC denial and unauthorized websocket handshake.
- Added first web store UI (React + Tailwind) with advanced lookup, cozy modern design, and game-image-first browsing.
- Integrated web store with live backend catalog via gateway API and realtime websocket updates.
- Added versioned backend catalog seeding command with seed-history tracking and compose one-off runner.
- Added `/games` static fallback integration: publish + sync scripts feeding `web/public/fallback-catalog.json` when gateway is unavailable.
- Added `web-store` service to Docker Compose for running frontend in the containerized stack.
- Added backend leaderboard APIs (global and per-game), developer score/hubcoin hooks, and web leaderboard UI integration.
- Added Tik-Toe full-stack upgrade: React/Tailwind/Bun game client + backend matchmaking/match/chat APIs.
