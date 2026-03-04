# HubGame TODO

## Global
- [ ] Add integration tests for auth + websocket stream through gateway proxy.
- [ ] Design SDK contract for client games (TypeScript package).
- [ ] Implement matchmaking + room orchestration service.
- [ ] Add CI workflow for tests/build/lint.

## Database Backbone
- [ ] Add snapshots + replay recovery APIs.
- [ ] Add retention and compaction strategy for event log.
- [ ] Add index advisor and query profiling metrics.
- [ ] Add batched write transaction API for hot game events.

## Controller Backbone
- [ ] Add refresh tokens and token revocation storage.
- [ ] Add API keys/service accounts for bot and CLI automation.
- [ ] Add encryption-at-rest strategy for sensitive entity fields.
- [ ] Add tenant-level policy DSL for advanced authorization.
