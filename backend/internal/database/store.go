package database

import (
	"context"
	"database/sql"
	"encoding/json"
	"errors"
	"fmt"
	"time"

	_ "modernc.org/sqlite"
)

var ErrNotFound = errors.New("not found")

type Store struct {
	db          *sql.DB
	broker      *Broker
	controllers []Controller
}

func OpenSQLite(ctx context.Context, dsn string) (*Store, error) {
	db, err := sql.Open("sqlite", dsn)
	if err != nil {
		return nil, err
	}

	if _, err := db.ExecContext(ctx, "PRAGMA journal_mode=WAL;"); err != nil {
		return nil, fmt.Errorf("enable wal: %w", err)
	}
	if _, err := db.ExecContext(ctx, "PRAGMA synchronous=NORMAL;"); err != nil {
		return nil, fmt.Errorf("set sync mode: %w", err)
	}

	s := &Store{db: db, broker: NewBroker(), controllers: []Controller{NopController{}}}
	if err := s.migrate(ctx); err != nil {
		return nil, err
	}
	return s, nil
}

func (s *Store) Close() error    { return s.db.Close() }
func (s *Store) Broker() *Broker { return s.broker }

func (s *Store) RegisterController(c Controller) {
	s.controllers = append(s.controllers, c)
}

func (s *Store) migrate(ctx context.Context) error {
	schema := `
CREATE TABLE IF NOT EXISTS entities (
	id TEXT NOT NULL,
	tenant_id TEXT NOT NULL,
	kind TEXT NOT NULL,
	data TEXT NOT NULL,
	version INTEGER NOT NULL DEFAULT 1,
	created_at TIMESTAMP NOT NULL,
	updated_at TIMESTAMP NOT NULL,
	deleted_at TIMESTAMP NULL,
	PRIMARY KEY (tenant_id, id)
);
CREATE INDEX IF NOT EXISTS idx_entities_tenant_kind ON entities(tenant_id, kind);

CREATE TABLE IF NOT EXISTS events (
	id INTEGER PRIMARY KEY AUTOINCREMENT,
	tenant_id TEXT NOT NULL,
	topic TEXT NOT NULL,
	key_ref TEXT NOT NULL,
	type TEXT NOT NULL,
	payload TEXT NOT NULL,
	created_at TIMESTAMP NOT NULL
);
CREATE INDEX IF NOT EXISTS idx_events_tenant_topic_id ON events(tenant_id, topic, id);
`
	_, err := s.db.ExecContext(ctx, schema)
	return err
}

func (s *Store) InsertEntity(ctx context.Context, e *Entity) error {
	now := time.Now().UTC()
	if len(e.Data) == 0 {
		e.Data = json.RawMessage(`{}`)
	}
	e.CreatedAt = now
	e.UpdatedAt = now
	e.Version = 1

	for _, c := range s.controllers {
		if err := c.BeforeInsert(ctx, e); err != nil {
			return fmt.Errorf("%s: %w", c.Name(), err)
		}
	}

	_, err := s.db.ExecContext(ctx, `
INSERT INTO entities(id, tenant_id, kind, data, version, created_at, updated_at, deleted_at)
VALUES (?, ?, ?, ?, ?, ?, ?, NULL)
`, e.ID, e.TenantID, e.Kind, string(e.Data), e.Version, e.CreatedAt, e.UpdatedAt)
	if err != nil {
		return err
	}
	_, _ = s.AppendEvent(ctx, Event{
		TenantID: e.TenantID,
		Topic:    "entity." + e.Kind,
		Key:      e.ID,
		Type:     "entity.inserted",
		Payload:  mustJSON(e),
	})
	return nil
}

func (s *Store) GetEntity(ctx context.Context, tenantID, id string) (*Entity, error) {
	var e Entity
	var data string
	var deleted sql.NullTime
	err := s.db.QueryRowContext(ctx, `
SELECT id, tenant_id, kind, data, version, created_at, updated_at, deleted_at
FROM entities WHERE tenant_id = ? AND id = ?
`, tenantID, id).Scan(
		&e.ID, &e.TenantID, &e.Kind, &data, &e.Version, &e.CreatedAt, &e.UpdatedAt, &deleted,
	)
	if errors.Is(err, sql.ErrNoRows) {
		return nil, ErrNotFound
	}
	if err != nil {
		return nil, err
	}
	e.Data = json.RawMessage(data)
	if deleted.Valid {
		e.DeletedAt = &deleted.Time
	}
	return &e, nil
}

func (s *Store) UpdateEntity(ctx context.Context, next *Entity) error {
	current, err := s.GetEntity(ctx, next.TenantID, next.ID)
	if err != nil {
		return err
	}
	next.Kind = current.Kind
	next.CreatedAt = current.CreatedAt
	next.Version = current.Version + 1
	next.UpdatedAt = time.Now().UTC()
	next.DeletedAt = current.DeletedAt

	for _, c := range s.controllers {
		if err := c.BeforeUpdate(ctx, current, next); err != nil {
			return fmt.Errorf("%s: %w", c.Name(), err)
		}
	}

	res, err := s.db.ExecContext(ctx, `
UPDATE entities
SET data = ?, version = ?, updated_at = ?
WHERE tenant_id = ? AND id = ? AND deleted_at IS NULL
`, string(next.Data), next.Version, next.UpdatedAt, next.TenantID, next.ID)
	if err != nil {
		return err
	}
	affected, _ := res.RowsAffected()
	if affected == 0 {
		return ErrNotFound
	}
	_, _ = s.AppendEvent(ctx, Event{
		TenantID: next.TenantID,
		Topic:    "entity." + next.Kind,
		Key:      next.ID,
		Type:     "entity.updated",
		Payload:  mustJSON(next),
	})
	return nil
}

func (s *Store) DeleteEntity(ctx context.Context, tenantID, id string) error {
	e, err := s.GetEntity(ctx, tenantID, id)
	if err != nil {
		return err
	}
	for _, c := range s.controllers {
		if err := c.BeforeDelete(ctx, e); err != nil {
			return fmt.Errorf("%s: %w", c.Name(), err)
		}
	}
	now := time.Now().UTC()
	res, err := s.db.ExecContext(ctx, `
UPDATE entities SET deleted_at = ?, updated_at = ?, version = version + 1
WHERE tenant_id = ? AND id = ? AND deleted_at IS NULL
`, now, now, tenantID, id)
	if err != nil {
		return err
	}
	affected, _ := res.RowsAffected()
	if affected == 0 {
		return ErrNotFound
	}
	_, _ = s.AppendEvent(ctx, Event{
		TenantID: tenantID,
		Topic:    "entity." + e.Kind,
		Key:      id,
		Type:     "entity.deleted",
		Payload:  mustJSON(map[string]string{"tenant_id": tenantID, "id": id}),
	})
	return nil
}

func (s *Store) ListEntities(ctx context.Context, tenantID, kind string, limit int) ([]Entity, error) {
	if limit <= 0 || limit > 200 {
		limit = 50
	}
	rows, err := s.db.QueryContext(ctx, `
SELECT id, tenant_id, kind, data, version, created_at, updated_at, deleted_at
FROM entities WHERE tenant_id = ? AND kind = ? AND deleted_at IS NULL
ORDER BY updated_at DESC
LIMIT ?
`, tenantID, kind, limit)
	if err != nil {
		return nil, err
	}
	defer rows.Close()

	out := make([]Entity, 0, limit)
	for rows.Next() {
		var e Entity
		var data string
		var deleted sql.NullTime
		if err := rows.Scan(&e.ID, &e.TenantID, &e.Kind, &data, &e.Version, &e.CreatedAt, &e.UpdatedAt, &deleted); err != nil {
			return nil, err
		}
		e.Data = json.RawMessage(data)
		if deleted.Valid {
			e.DeletedAt = &deleted.Time
		}
		out = append(out, e)
	}
	return out, rows.Err()
}

func (s *Store) AppendEvent(ctx context.Context, event Event) (Event, error) {
	if len(event.Payload) == 0 {
		event.Payload = json.RawMessage(`{}`)
	}
	event.CreatedAt = time.Now().UTC()
	for _, c := range s.controllers {
		if err := c.BeforeAppendEvent(ctx, &event); err != nil {
			return Event{}, fmt.Errorf("%s: %w", c.Name(), err)
		}
	}

	res, err := s.db.ExecContext(ctx, `
INSERT INTO events(tenant_id, topic, key_ref, type, payload, created_at)
VALUES (?, ?, ?, ?, ?, ?)
`, event.TenantID, event.Topic, event.Key, event.Type, string(event.Payload), event.CreatedAt)
	if err != nil {
		return Event{}, err
	}
	event.ID, _ = res.LastInsertId()
	s.broker.Publish(event.Topic, event)
	return event, nil
}

func (s *Store) ListEvents(ctx context.Context, tenantID, topic string, afterID int64, limit int) ([]Event, error) {
	if limit <= 0 || limit > 500 {
		limit = 100
	}
	rows, err := s.db.QueryContext(ctx, `
SELECT id, tenant_id, topic, key_ref, type, payload, created_at
FROM events
WHERE tenant_id = ? AND topic = ? AND id > ?
ORDER BY id ASC
LIMIT ?
`, tenantID, topic, afterID, limit)
	if err != nil {
		return nil, err
	}
	defer rows.Close()

	out := make([]Event, 0, limit)
	for rows.Next() {
		var ev Event
		var payload string
		if err := rows.Scan(&ev.ID, &ev.TenantID, &ev.Topic, &ev.Key, &ev.Type, &payload, &ev.CreatedAt); err != nil {
			return nil, err
		}
		ev.Payload = json.RawMessage(payload)
		out = append(out, ev)
	}
	return out, rows.Err()
}

func mustJSON(v any) json.RawMessage {
	b, err := json.Marshal(v)
	if err != nil {
		return json.RawMessage(`{"error":"marshal"}`)
	}
	return b
}
