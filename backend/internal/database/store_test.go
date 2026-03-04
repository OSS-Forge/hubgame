package database

import (
	"context"
	"encoding/json"
	"testing"
)

func TestStoreCRUDAndEvents(t *testing.T) {
	ctx := context.Background()
	store, err := OpenSQLite(ctx, "file::memory:?cache=shared")
	if err != nil {
		t.Fatalf("open: %v", err)
	}
	defer store.Close()

	e := &Entity{
		ID:       "u-1",
		TenantID: "t-1",
		Kind:     "user",
		Data:     json.RawMessage(`{"name":"Ada"}`),
	}
	if err := store.InsertEntity(ctx, e); err != nil {
		t.Fatalf("insert: %v", err)
	}

	got, err := store.GetEntity(ctx, "t-1", "u-1")
	if err != nil {
		t.Fatalf("get: %v", err)
	}
	if got.Version != 1 {
		t.Fatalf("expected version 1, got %d", got.Version)
	}

	if err := store.UpdateEntity(ctx, &Entity{
		ID:       "u-1",
		TenantID: "t-1",
		Data:     json.RawMessage(`{"name":"Ada Lovelace"}`),
	}); err != nil {
		t.Fatalf("update: %v", err)
	}

	if err := store.DeleteEntity(ctx, "t-1", "u-1"); err != nil {
		t.Fatalf("delete: %v", err)
	}

	events, err := store.ListEvents(ctx, "t-1", "entity.user", 0, 100)
	if err != nil {
		t.Fatalf("events: %v", err)
	}
	if len(events) != 3 {
		t.Fatalf("expected 3 events, got %d", len(events))
	}
}
