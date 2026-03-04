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
