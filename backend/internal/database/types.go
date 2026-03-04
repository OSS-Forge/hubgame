package database

import (
	"context"
	"encoding/json"
	"time"
)

// Entity is the multi-tenant record unit for platform services.
type Entity struct {
	ID        string          `json:"id"`
	TenantID  string          `json:"tenant_id"`
	Kind      string          `json:"kind"`
	Data      json.RawMessage `json:"data"`
	Version   int64           `json:"version"`
	CreatedAt time.Time       `json:"created_at"`
	UpdatedAt time.Time       `json:"updated_at"`
	DeletedAt *time.Time      `json:"deleted_at,omitempty"`
}

// Event is the append-only unit for room/match/chat state transitions.
type Event struct {
	ID        int64           `json:"id"`
	TenantID  string          `json:"tenant_id"`
	Topic     string          `json:"topic"`
	Key       string          `json:"key"`
	Type      string          `json:"type"`
	Payload   json.RawMessage `json:"payload"`
	CreatedAt time.Time       `json:"created_at"`
}

// Controller allows embedding cross-cutting behavior in the storage layer.
type Controller interface {
	Name() string
	BeforeInsert(ctx context.Context, e *Entity) error
	BeforeUpdate(ctx context.Context, current, next *Entity) error
	BeforeDelete(ctx context.Context, e *Entity) error
	BeforeAppendEvent(ctx context.Context, event *Event) error
}

// NopController is useful for tests and defaults.
type NopController struct{}

func (NopController) Name() string                                         { return "nop" }
func (NopController) BeforeInsert(context.Context, *Entity) error          { return nil }
func (NopController) BeforeUpdate(context.Context, *Entity, *Entity) error { return nil }
func (NopController) BeforeDelete(context.Context, *Entity) error          { return nil }
func (NopController) BeforeAppendEvent(context.Context, *Event) error      { return nil }
