package api

import (
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
	store      *database.Store
	auth       *controller.AuthController
	authorizer *controller.Authorizer
	ws         *realtime.Handler
}

func NewServer(store *database.Store, auth *controller.AuthController) *Server {
	return &Server{
		store:      store,
		auth:       auth,
		authorizer: controller.NewAuthorizer(),
		ws:         realtime.NewHandler(store.Broker()),
	}
}

func (s *Server) Router() http.Handler {
	mux := http.NewServeMux()
	mux.HandleFunc("/healthz", func(w http.ResponseWriter, _ *http.Request) {
		writeJSON(w, http.StatusOK, map[string]string{"status": "ok"})
	})

	mux.Handle("/v1/events/stream", s.auth.RequireAuth(s.requireAction(controller.ActionStreamRead, http.HandlerFunc(s.ws.Stream))))
	mux.Handle("/v1/entities", s.auth.RequireAuth(http.HandlerFunc(s.entitiesHandler)))
	mux.Handle("/v1/entities/", s.auth.RequireAuth(http.HandlerFunc(s.entityByIDHandler)))
	mux.Handle("/v1/events", s.auth.RequireAuth(http.HandlerFunc(s.eventsHandler)))
	mux.Handle("/v1/leaderboard", s.auth.RequireAuth(http.HandlerFunc(s.leaderboardHandler)))
	mux.Handle("/v1/leaderboard/users", s.auth.RequireAuth(http.HandlerFunc(s.leaderboardUsersHandler)))
	mux.Handle("/v1/leaderboard/scores", s.auth.RequireAuth(http.HandlerFunc(s.leaderboardScoresHandler)))
	mux.Handle("/v1/tiktoe/matches", s.auth.RequireAuth(http.HandlerFunc(s.tiktoeMatchesHandler)))
	mux.Handle("/v1/tiktoe/matches/", s.auth.RequireAuth(http.HandlerFunc(s.tiktoeMatchByIDHandler)))
	mux.Handle("/v1/tiktoe/matchmaking/enqueue", s.auth.RequireAuth(http.HandlerFunc(s.tiktoeMatchmakingEnqueueHandler)))
	mux.Handle("/v1/tiktoe/matchmaking/status", s.auth.RequireAuth(http.HandlerFunc(s.tiktoeMatchmakingStatusHandler)))
	mux.Handle("/v1/tiktoe/presence", s.auth.RequireAuth(http.HandlerFunc(s.tiktoePresenceHandler)))
	mux.Handle("/v1/tiktoe/players", s.auth.RequireAuth(http.HandlerFunc(s.tiktoePlayersHandler)))

	return withCORS(withRequestDebug("server", mux))
}

func (s *Server) entitiesHandler(w http.ResponseWriter, r *http.Request) {
	claims, _ := controller.ClaimsFromContext(r.Context())
	switch r.Method {
	case http.MethodGet:
		if err := s.authorizer.Enforce(claims, controller.ActionEntityRead); err != nil {
			http.Error(w, err.Error(), http.StatusForbidden)
			return
		}
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
		if err := s.authorizer.Enforce(claims, controller.ActionEntityWrite); err != nil {
			http.Error(w, err.Error(), http.StatusForbidden)
			return
		}
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
		ctx := withClaimsForStorage(r.Context(), claims)
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
		if err := s.authorizer.Enforce(claims, controller.ActionEntityRead); err != nil {
			http.Error(w, err.Error(), http.StatusForbidden)
			return
		}
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
		if err := s.authorizer.Enforce(claims, controller.ActionEntityWrite); err != nil {
			http.Error(w, err.Error(), http.StatusForbidden)
			return
		}
		var req struct {
			Data json.RawMessage `json:"data"`
		}
		if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
			http.Error(w, "invalid json", http.StatusBadRequest)
			return
		}
		e := &database.Entity{ID: id, TenantID: claims.TenantID, Data: req.Data}
		expectedVersion, hasExpectedVersion, err := parseIfMatchVersion(r.Header.Get("If-Match"))
		if err != nil {
			http.Error(w, err.Error(), http.StatusBadRequest)
			return
		}
		ctx := withClaimsForStorage(r.Context(), claims)
		if hasExpectedVersion {
			err = s.store.UpdateEntityWithVersion(ctx, e, &expectedVersion)
		} else {
			err = s.store.UpdateEntity(ctx, e)
		}
		if errors.Is(err, database.ErrVersionConflict) {
			http.Error(w, err.Error(), http.StatusConflict)
			return
		}
		if err != nil {
			http.Error(w, err.Error(), http.StatusBadRequest)
			return
		}
		writeJSON(w, http.StatusOK, map[string]string{"status": "updated"})
	case http.MethodDelete:
		if err := s.authorizer.Enforce(claims, controller.ActionEntityDelete); err != nil {
			http.Error(w, err.Error(), http.StatusForbidden)
			return
		}
		ctx := withClaimsForStorage(r.Context(), claims)
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
		if err := s.authorizer.Enforce(claims, controller.ActionEventRead); err != nil {
			http.Error(w, err.Error(), http.StatusForbidden)
			return
		}
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
		if err := s.authorizer.Enforce(claims, controller.ActionEventWrite); err != nil {
			http.Error(w, err.Error(), http.StatusForbidden)
			return
		}
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
		ctx := withClaimsForStorage(r.Context(), claims)
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

func (s *Server) leaderboardUsersHandler(w http.ResponseWriter, r *http.Request) {
	claims, _ := controller.ClaimsFromContext(r.Context())
	if err := s.authorizer.Enforce(claims, controller.ActionLeaderboardWrite); err != nil {
		http.Error(w, err.Error(), http.StatusForbidden)
		return
	}
	if r.Method != http.MethodPost {
		http.Error(w, "method not allowed", http.StatusMethodNotAllowed)
		return
	}
	var req leaderboardUserRequest
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		http.Error(w, "invalid json", http.StatusBadRequest)
		return
	}
	user, err := upsertLeaderboardUser(withClaimsForStorage(r.Context(), claims), s.store, claims.TenantID, req)
	if err != nil {
		http.Error(w, err.Error(), http.StatusBadRequest)
		return
	}
	writeJSON(w, http.StatusOK, user)
}

func (s *Server) leaderboardScoresHandler(w http.ResponseWriter, r *http.Request) {
	claims, _ := controller.ClaimsFromContext(r.Context())
	if err := s.authorizer.Enforce(claims, controller.ActionLeaderboardWrite); err != nil {
		http.Error(w, err.Error(), http.StatusForbidden)
		return
	}
	if r.Method != http.MethodPost {
		http.Error(w, "method not allowed", http.StatusMethodNotAllowed)
		return
	}
	var req leaderboardScoreRequest
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		http.Error(w, "invalid json", http.StatusBadRequest)
		return
	}
	score, err := submitLeaderboardScore(withClaimsForStorage(r.Context(), claims), s.store, claims.TenantID, req)
	if err != nil {
		http.Error(w, err.Error(), http.StatusBadRequest)
		return
	}
	writeJSON(w, http.StatusCreated, score)
}

func (s *Server) leaderboardHandler(w http.ResponseWriter, r *http.Request) {
	claims, _ := controller.ClaimsFromContext(r.Context())
	if err := s.authorizer.Enforce(claims, controller.ActionLeaderboardRead); err != nil {
		http.Error(w, err.Error(), http.StatusForbidden)
		return
	}
	if r.Method != http.MethodGet {
		http.Error(w, "method not allowed", http.StatusMethodNotAllowed)
		return
	}
	scope := strings.TrimSpace(r.URL.Query().Get("scope"))
	if scope == "" {
		scope = "global"
	}
	gameID := strings.TrimSpace(r.URL.Query().Get("game_id"))
	if scope == "game" && gameID == "" {
		http.Error(w, "game_id is required when scope=game", http.StatusBadRequest)
		return
	}
	limit, _ := strconv.Atoi(r.URL.Query().Get("limit"))
	rows, err := queryLeaderboard(withClaimsForStorage(r.Context(), claims), s.store, claims.TenantID, scope, gameID, limit)
	if err != nil {
		http.Error(w, err.Error(), http.StatusInternalServerError)
		return
	}
	writeJSON(w, http.StatusOK, map[string]any{
		"scope":   scope,
		"game_id": gameID,
		"items":   rows,
	})
}

func (s *Server) tiktoeMatchesHandler(w http.ResponseWriter, r *http.Request) {
	claims, _ := controller.ClaimsFromContext(r.Context())
	if err := s.authorizer.Enforce(claims, controller.ActionEntityWrite); err != nil {
		http.Error(w, err.Error(), http.StatusForbidden)
		return
	}
	if r.Method != http.MethodPost {
		http.Error(w, "method not allowed", http.StatusMethodNotAllowed)
		return
	}
	var req createMatchRequest
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		http.Error(w, "invalid json", http.StatusBadRequest)
		return
	}
	match, err := createTiktoeMatch(withClaimsForStorage(r.Context(), claims), s.store, claims.TenantID, req)
	if err != nil {
		http.Error(w, err.Error(), http.StatusBadRequest)
		return
	}
	writeJSON(w, http.StatusCreated, match)
}

func (s *Server) tiktoeMatchByIDHandler(w http.ResponseWriter, r *http.Request) {
	claims, _ := controller.ClaimsFromContext(r.Context())
	path := strings.TrimPrefix(r.URL.Path, "/v1/tiktoe/matches/")
	parts := strings.Split(strings.Trim(path, "/"), "/")
	if len(parts) == 0 || parts[0] == "" {
		http.Error(w, "match id is required", http.StatusBadRequest)
		return
	}
	matchID := parts[0]

	if len(parts) == 1 && r.Method == http.MethodGet {
		if err := s.authorizer.Enforce(claims, controller.ActionEntityRead); err != nil {
			http.Error(w, err.Error(), http.StatusForbidden)
			return
		}
		match, err := loadTiktoeMatch(withClaimsForStorage(r.Context(), claims), s.store, claims.TenantID, matchID)
		if errors.Is(err, database.ErrNotFound) {
			http.Error(w, "not found", http.StatusNotFound)
			return
		}
		if err != nil {
			http.Error(w, err.Error(), http.StatusBadRequest)
			return
		}
		writeJSON(w, http.StatusOK, match)
		return
	}

	if len(parts) == 2 && parts[1] == "moves" && r.Method == http.MethodPost {
		if err := s.authorizer.Enforce(claims, controller.ActionEntityWrite); err != nil {
			http.Error(w, err.Error(), http.StatusForbidden)
			return
		}
		var req moveRequest
		if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
			http.Error(w, "invalid json", http.StatusBadRequest)
			return
		}
		match, err := applyTiktoeMove(withClaimsForStorage(r.Context(), claims), s.store, claims.TenantID, matchID, req)
		if err != nil {
			http.Error(w, err.Error(), http.StatusBadRequest)
			return
		}
		writeJSON(w, http.StatusOK, match)
		return
	}

	if len(parts) == 2 && parts[1] == "chat" {
		if r.Method == http.MethodGet {
			if err := s.authorizer.Enforce(claims, controller.ActionEventRead); err != nil {
				http.Error(w, err.Error(), http.StatusForbidden)
				return
			}
			limit, _ := strconv.Atoi(r.URL.Query().Get("limit"))
			items, err := listTiktoeChat(withClaimsForStorage(r.Context(), claims), s.store, claims.TenantID, matchID, limit)
			if err != nil {
				http.Error(w, err.Error(), http.StatusInternalServerError)
				return
			}
			writeJSON(w, http.StatusOK, items)
			return
		}
		if r.Method == http.MethodPost {
			if err := s.authorizer.Enforce(claims, controller.ActionEventWrite); err != nil {
				http.Error(w, err.Error(), http.StatusForbidden)
				return
			}
			var req chatRequest
			if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
				http.Error(w, "invalid json", http.StatusBadRequest)
				return
			}
			msg, err := postTiktoeChat(withClaimsForStorage(r.Context(), claims), s.store, claims.TenantID, matchID, req)
			if err != nil {
				http.Error(w, err.Error(), http.StatusBadRequest)
				return
			}
			writeJSON(w, http.StatusCreated, msg)
			return
		}
	}
	http.Error(w, "not found", http.StatusNotFound)
}

func (s *Server) tiktoeMatchmakingEnqueueHandler(w http.ResponseWriter, r *http.Request) {
	claims, _ := controller.ClaimsFromContext(r.Context())
	if err := s.authorizer.Enforce(claims, controller.ActionEntityWrite); err != nil {
		http.Error(w, err.Error(), http.StatusForbidden)
		return
	}
	if r.Method != http.MethodPost {
		http.Error(w, "method not allowed", http.StatusMethodNotAllowed)
		return
	}
	var req enqueueRequest
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		http.Error(w, "invalid json", http.StatusBadRequest)
		return
	}
	result, err := enqueueTiktoe(withClaimsForStorage(r.Context(), claims), s.store, claims.TenantID, req)
	if err != nil {
		http.Error(w, err.Error(), http.StatusBadRequest)
		return
	}
	writeJSON(w, http.StatusOK, result)
}

func (s *Server) tiktoeMatchmakingStatusHandler(w http.ResponseWriter, r *http.Request) {
	claims, _ := controller.ClaimsFromContext(r.Context())
	if err := s.authorizer.Enforce(claims, controller.ActionEntityRead); err != nil {
		http.Error(w, err.Error(), http.StatusForbidden)
		return
	}
	if r.Method != http.MethodGet {
		http.Error(w, "method not allowed", http.StatusMethodNotAllowed)
		return
	}
	userID := strings.TrimSpace(r.URL.Query().Get("user_id"))
	if userID == "" {
		http.Error(w, "user_id is required", http.StatusBadRequest)
		return
	}
	boardSize, _ := strconv.Atoi(r.URL.Query().Get("board_size"))
	winLength, _ := strconv.Atoi(r.URL.Query().Get("win_length"))
	status, err := tiktoeQueueStatus(withClaimsForStorage(r.Context(), claims), s.store, claims.TenantID, userID, boardSize, winLength)
	if err != nil {
		http.Error(w, err.Error(), http.StatusInternalServerError)
		return
	}
	writeJSON(w, http.StatusOK, status)
}

func (s *Server) tiktoePresenceHandler(w http.ResponseWriter, r *http.Request) {
	claims, _ := controller.ClaimsFromContext(r.Context())
	if err := s.authorizer.Enforce(claims, controller.ActionEntityWrite); err != nil {
		http.Error(w, err.Error(), http.StatusForbidden)
		return
	}
	if r.Method != http.MethodPost {
		http.Error(w, "method not allowed", http.StatusMethodNotAllowed)
		return
	}
	var req presenceRequest
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		http.Error(w, "invalid json", http.StatusBadRequest)
		return
	}
	presence, err := upsertTiktoePresence(withClaimsForStorage(r.Context(), claims), s.store, claims.TenantID, req)
	if err != nil {
		http.Error(w, err.Error(), http.StatusBadRequest)
		return
	}
	writeJSON(w, http.StatusOK, presence)
}

func (s *Server) tiktoePlayersHandler(w http.ResponseWriter, r *http.Request) {
	claims, _ := controller.ClaimsFromContext(r.Context())
	if err := s.authorizer.Enforce(claims, controller.ActionEntityRead); err != nil {
		http.Error(w, err.Error(), http.StatusForbidden)
		return
	}
	if r.Method != http.MethodGet {
		http.Error(w, "method not allowed", http.StatusMethodNotAllowed)
		return
	}
	limit, _ := strconv.Atoi(r.URL.Query().Get("limit"))
	items, err := listTiktoePresence(
		withClaimsForStorage(r.Context(), claims),
		s.store,
		claims.TenantID,
		r.URL.Query().Get("exclude_user_id"),
		r.URL.Query().Get("q"),
		limit,
	)
	if err != nil {
		http.Error(w, err.Error(), http.StatusInternalServerError)
		return
	}
	writeJSON(w, http.StatusOK, items)
}

func logging(next http.Handler) http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		start := time.Now()
		next.ServeHTTP(w, r)
		_ = start
	})
}

func (s *Server) requireAction(action string, next http.Handler) http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		claims, _ := controller.ClaimsFromContext(r.Context())
		if err := s.authorizer.Enforce(claims, action); err != nil {
			http.Error(w, err.Error(), http.StatusForbidden)
			return
		}
		next.ServeHTTP(w, r)
	})
}

func writeJSON(w http.ResponseWriter, status int, v any) {
	w.Header().Set("Content-Type", "application/json")
	w.WriteHeader(status)
	_ = json.NewEncoder(w).Encode(v)
}

func parseIfMatchVersion(v string) (int64, bool, error) {
	v = strings.TrimSpace(v)
	if v == "" {
		return 0, false, nil
	}
	v = strings.TrimPrefix(v, "W/")
	v = strings.Trim(v, "\"")
	parsed, err := strconv.ParseInt(v, 10, 64)
	if err != nil {
		return 0, false, errors.New("If-Match must be a numeric version")
	}
	return parsed, true, nil
}
