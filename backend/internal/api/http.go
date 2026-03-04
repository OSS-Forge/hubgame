package api

import (
	"context"
	"encoding/json"
	"errors"
	"net/http"
	"strconv"
	"strings"
	"time"

	"hubgame/backend/internal/controller"
	"hubgame/backend/internal/database"
	"hubgame/backend/internal/realtime"
)

type Server struct {
	store *database.Store
	auth  *controller.AuthController
	ws    *realtime.Handler
}

func NewServer(store *database.Store, auth *controller.AuthController) *Server {
	return &Server{store: store, auth: auth, ws: realtime.NewHandler(store.Broker())}
}

func (s *Server) Router() http.Handler {
	mux := http.NewServeMux()
	mux.HandleFunc("/healthz", func(w http.ResponseWriter, _ *http.Request) {
		writeJSON(w, http.StatusOK, map[string]string{"status": "ok"})
	})

	mux.Handle("/v1/events/stream", s.auth.RequireAuth(http.HandlerFunc(s.ws.Stream)))
	mux.Handle("/v1/entities", s.auth.RequireAuth(http.HandlerFunc(s.entitiesHandler)))
	mux.Handle("/v1/entities/", s.auth.RequireAuth(http.HandlerFunc(s.entityByIDHandler)))
	mux.Handle("/v1/events", s.auth.RequireAuth(http.HandlerFunc(s.eventsHandler)))

	return logging(mux)
}

func (s *Server) entitiesHandler(w http.ResponseWriter, r *http.Request) {
	claims, _ := controller.ClaimsFromContext(r.Context())
	switch r.Method {
	case http.MethodGet:
		kind := r.URL.Query().Get("kind")
		if kind == "" {
			http.Error(w, "kind is required", http.StatusBadRequest)
			return
		}
		limit, _ := strconv.Atoi(r.URL.Query().Get("limit"))
		items, err := s.store.ListEntities(r.Context(), claims.TenantID, kind, limit)
		if err != nil {
			http.Error(w, err.Error(), http.StatusInternalServerError)
			return
		}
		writeJSON(w, http.StatusOK, items)
	case http.MethodPost:
		var req struct {
			ID   string          `json:"id"`
			Kind string          `json:"kind"`
			Data json.RawMessage `json:"data"`
		}
		if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
			http.Error(w, "invalid json", http.StatusBadRequest)
			return
		}
		e := &database.Entity{ID: req.ID, TenantID: claims.TenantID, Kind: req.Kind, Data: req.Data}
		ctx := context.WithValue(r.Context(), contextKeyClaims{}, claims)
		if err := s.store.InsertEntity(ctx, e); err != nil {
			http.Error(w, err.Error(), http.StatusBadRequest)
			return
		}
		writeJSON(w, http.StatusCreated, e)
	default:
		http.Error(w, "method not allowed", http.StatusMethodNotAllowed)
	}
}

func (s *Server) entityByIDHandler(w http.ResponseWriter, r *http.Request) {
	claims, _ := controller.ClaimsFromContext(r.Context())
	id := strings.TrimPrefix(r.URL.Path, "/v1/entities/")
	if id == "" {
		http.Error(w, "id is required", http.StatusBadRequest)
		return
	}

	switch r.Method {
	case http.MethodGet:
		e, err := s.store.GetEntity(r.Context(), claims.TenantID, id)
		if errors.Is(err, database.ErrNotFound) {
			http.Error(w, "not found", http.StatusNotFound)
			return
		}
		if err != nil {
			http.Error(w, err.Error(), http.StatusInternalServerError)
			return
		}
		writeJSON(w, http.StatusOK, e)
	case http.MethodPatch:
		var req struct {
			Data json.RawMessage `json:"data"`
		}
		if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
			http.Error(w, "invalid json", http.StatusBadRequest)
			return
		}
		e := &database.Entity{ID: id, TenantID: claims.TenantID, Data: req.Data}
		ctx := context.WithValue(r.Context(), contextKeyClaims{}, claims)
		if err := s.store.UpdateEntity(ctx, e); err != nil {
			http.Error(w, err.Error(), http.StatusBadRequest)
			return
		}
		writeJSON(w, http.StatusOK, map[string]string{"status": "updated"})
	case http.MethodDelete:
		ctx := context.WithValue(r.Context(), contextKeyClaims{}, claims)
		if err := s.store.DeleteEntity(ctx, claims.TenantID, id); err != nil {
			http.Error(w, err.Error(), http.StatusBadRequest)
			return
		}
		writeJSON(w, http.StatusOK, map[string]string{"status": "deleted"})
	default:
		http.Error(w, "method not allowed", http.StatusMethodNotAllowed)
	}
}

func (s *Server) eventsHandler(w http.ResponseWriter, r *http.Request) {
	claims, _ := controller.ClaimsFromContext(r.Context())
	switch r.Method {
	case http.MethodGet:
		topic := r.URL.Query().Get("topic")
		if topic == "" {
			http.Error(w, "topic is required", http.StatusBadRequest)
			return
		}
		afterID, _ := strconv.ParseInt(r.URL.Query().Get("after_id"), 10, 64)
		limit, _ := strconv.Atoi(r.URL.Query().Get("limit"))
		events, err := s.store.ListEvents(r.Context(), claims.TenantID, topic, afterID, limit)
		if err != nil {
			http.Error(w, err.Error(), http.StatusInternalServerError)
			return
		}
		writeJSON(w, http.StatusOK, events)
	case http.MethodPost:
		var req struct {
			Topic   string          `json:"topic"`
			Key     string          `json:"key"`
			Type    string          `json:"type"`
			Payload json.RawMessage `json:"payload"`
		}
		if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
			http.Error(w, "invalid json", http.StatusBadRequest)
			return
		}
		ctx := context.WithValue(r.Context(), contextKeyClaims{}, claims)
		ev, err := s.store.AppendEvent(ctx, database.Event{
			TenantID: claims.TenantID,
			Topic:    req.Topic,
			Key:      req.Key,
			Type:     req.Type,
			Payload:  req.Payload,
		})
		if err != nil {
			http.Error(w, err.Error(), http.StatusBadRequest)
			return
		}
		writeJSON(w, http.StatusCreated, ev)
	default:
		http.Error(w, "method not allowed", http.StatusMethodNotAllowed)
	}
}

type contextKeyClaims struct{}

func logging(next http.Handler) http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		start := time.Now()
		next.ServeHTTP(w, r)
		_ = start
	})
}

func writeJSON(w http.ResponseWriter, status int, v any) {
	w.Header().Set("Content-Type", "application/json")
	w.WriteHeader(status)
	_ = json.NewEncoder(w).Encode(v)
}
