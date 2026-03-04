# Database Progress

## 2026-03-04
- Added SQLite schema initialization (entities + events).
- Implemented CRUD operations with soft-delete and version increment.
- Added append-only event storage and topic-based pub/sub broker.
- Auto-emitted entity lifecycle events: inserted, updated, deleted.
