# HubGame TODO

## Global
- [ ] Add Docker Compose for gateway + db-engine + controller split services.
- [ ] Add integration tests for auth + websocket event streaming.
- [ ] Design SDK contract for client games (TypeScript package).
- [ ] Implement matchmaking + room orchestration service.

## Database Backbone
- [ ] Add optimistic concurrency with `If-Match`/expected version checks.
- [ ] Add snapshots + replay recovery APIs.
- [ ] Add retention and compaction strategy for event log.
- [ ] Add index advisor and query profiling metrics.

## Controller Backbone
- [ ] Add refresh tokens and token revocation storage.
- [ ] Add RBAC policies beyond tenant isolation.
- [ ] Add API keys/service accounts for bot and CLI automation.
- [ ] Add encryption-at-rest strategy for sensitive entity fields.
