package api

import (
	"encoding/json"
	"errors"
	"net/http"
	"strconv"
	"strings"

	"hubgame/backend/internal/database"
	"hubgame/backend/internal/realtime"
)

type DBEngineServer struct {
	store         *database.Store
	internalToken string
	ws            *realtime.Handler
}

func NewDBEngineServer(store *database.Store, internalToken string) *DBEngineServer {
	return &DBEngineServer{store: store, internalToken: internalToken, ws: realtime.NewHandler(store.Broker())}
}

func (s *DBEngineServer) Router() http.Handler {
	mux := http.NewServeMux()
	mux.HandleFunc("/healthz", func(w http.ResponseWriter, _ *http.Request) {
		writeJSON(w, http.StatusOK, map[string]string{"status": "ok", "service": "db-engine"})
	})
	mux.Handle("/v1/events/stream", s.requireInternal(http.HandlerFunc(s.ws.Stream)))
	mux.Handle("/v1/entities", s.requireInternal(http.HandlerFunc(s.entitiesHandler)))
	mux.Handle("/v1/entities/", s.requireInternal(http.HandlerFunc(s.entityByIDHandler)))
	mux.Handle("/v1/events", s.requireInternal(http.HandlerFunc(s.eventsHandler)))
	mux.Handle("/v1/leaderboard", s.requireInternal(http.HandlerFunc(s.leaderboardHandler)))
	mux.Handle("/v1/leaderboard/users", s.requireInternal(http.HandlerFunc(s.leaderboardUsersHandler)))
	mux.Handle("/v1/leaderboard/scores", s.requireInternal(http.HandlerFunc(s.leaderboardScoresHandler)))
	mux.Handle("/v1/tiktoe/matches", s.requireInternal(http.HandlerFunc(s.tiktoeMatchesHandler)))
	mux.Handle("/v1/tiktoe/matches/", s.requireInternal(http.HandlerFunc(s.tiktoeMatchByIDHandler)))
	mux.Handle("/v1/tiktoe/matchmaking/enqueue", s.requireInternal(http.HandlerFunc(s.tiktoeMatchmakingEnqueueHandler)))
	mux.Handle("/v1/tiktoe/matchmaking/status", s.requireInternal(http.HandlerFunc(s.tiktoeMatchmakingStatusHandler)))
	mux.Handle("/v1/tiktoe/presence", s.requireInternal(http.HandlerFunc(s.tiktoePresenceHandler)))
	mux.Handle("/v1/tiktoe/players", s.requireInternal(http.HandlerFunc(s.tiktoePlayersHandler)))
	return withCORS(withRequestDebug("db-engine", mux))
}

func (s *DBEngineServer) requireInternal(next http.Handler) http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.Header.Get("X-Internal-Token") != s.internalToken {
			http.Error(w, "unauthorized", http.StatusUnauthorized)
			return
		}
		next.ServeHTTP(w, r)
	})
}

func (s *DBEngineServer) tenantID(r *http.Request) (string, error) {
	tenantID := strings.TrimSpace(r.Header.Get("X-Tenant-ID"))
	if tenantID == "" {
		return "", errors.New("X-Tenant-ID is required")
	}
	return tenantID, nil
}

func (s *DBEngineServer) entitiesHandler(w http.ResponseWriter, r *http.Request) {
	tenantID, err := s.tenantID(r)
	if err != nil {
		http.Error(w, err.Error(), http.StatusBadRequest)
		return
	}
	switch r.Method {
	case http.MethodGet:
		kind := r.URL.Query().Get("kind")
		if kind == "" {
			http.Error(w, "kind is required", http.StatusBadRequest)
			return
		}
		limit, _ := strconv.Atoi(r.URL.Query().Get("limit"))
		items, err := s.store.ListEntities(r.Context(), tenantID, kind, limit)
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
		e := &database.Entity{ID: req.ID, TenantID: tenantID, Kind: req.Kind, Data: req.Data}
		if err := s.store.InsertEntity(r.Context(), e); err != nil {
			http.Error(w, err.Error(), http.StatusBadRequest)
			return
		}
		writeJSON(w, http.StatusCreated, e)
	default:
		http.Error(w, "method not allowed", http.StatusMethodNotAllowed)
	}
}

func (s *DBEngineServer) entityByIDHandler(w http.ResponseWriter, r *http.Request) {
	tenantID, err := s.tenantID(r)
	if err != nil {
		http.Error(w, err.Error(), http.StatusBadRequest)
		return
	}
	id := strings.TrimPrefix(r.URL.Path, "/v1/entities/")
	if id == "" {
		http.Error(w, "id is required", http.StatusBadRequest)
		return
	}

	switch r.Method {
	case http.MethodGet:
		e, err := s.store.GetEntity(r.Context(), tenantID, id)
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
		e := &database.Entity{ID: id, TenantID: tenantID, Data: req.Data}
		expectedVersion, hasExpectedVersion, err := parseIfMatchVersion(r.Header.Get("If-Match"))
		if err != nil {
			http.Error(w, err.Error(), http.StatusBadRequest)
			return
		}
		if hasExpectedVersion {
			err = s.store.UpdateEntityWithVersion(r.Context(), e, &expectedVersion)
		} else {
			err = s.store.UpdateEntity(r.Context(), e)
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
		if err := s.store.DeleteEntity(r.Context(), tenantID, id); err != nil {
			http.Error(w, err.Error(), http.StatusBadRequest)
			return
		}
		writeJSON(w, http.StatusOK, map[string]string{"status": "deleted"})
	default:
		http.Error(w, "method not allowed", http.StatusMethodNotAllowed)
	}
}

func (s *DBEngineServer) eventsHandler(w http.ResponseWriter, r *http.Request) {
	tenantID, err := s.tenantID(r)
	if err != nil {
		http.Error(w, err.Error(), http.StatusBadRequest)
		return
	}
	switch r.Method {
	case http.MethodGet:
		topic := r.URL.Query().Get("topic")
		if topic == "" {
			http.Error(w, "topic is required", http.StatusBadRequest)
			return
		}
		afterID, _ := strconv.ParseInt(r.URL.Query().Get("after_id"), 10, 64)
		limit, _ := strconv.Atoi(r.URL.Query().Get("limit"))
		events, err := s.store.ListEvents(r.Context(), tenantID, topic, afterID, limit)
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
		ev, err := s.store.AppendEvent(r.Context(), database.Event{
			TenantID: tenantID,
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

func (s *DBEngineServer) leaderboardUsersHandler(w http.ResponseWriter, r *http.Request) {
	tenantID, err := s.tenantID(r)
	if err != nil {
		http.Error(w, err.Error(), http.StatusBadRequest)
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
	user, err := upsertLeaderboardUser(r.Context(), s.store, tenantID, req)
	if err != nil {
		http.Error(w, err.Error(), http.StatusBadRequest)
		return
	}
	writeJSON(w, http.StatusOK, user)
}

func (s *DBEngineServer) leaderboardScoresHandler(w http.ResponseWriter, r *http.Request) {
	tenantID, err := s.tenantID(r)
	if err != nil {
		http.Error(w, err.Error(), http.StatusBadRequest)
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
	score, err := submitLeaderboardScore(r.Context(), s.store, tenantID, req)
	if err != nil {
		http.Error(w, err.Error(), http.StatusBadRequest)
		return
	}
	writeJSON(w, http.StatusCreated, score)
}

func (s *DBEngineServer) leaderboardHandler(w http.ResponseWriter, r *http.Request) {
	tenantID, err := s.tenantID(r)
	if err != nil {
		http.Error(w, err.Error(), http.StatusBadRequest)
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
	rows, err := queryLeaderboard(r.Context(), s.store, tenantID, scope, gameID, limit)
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

func (s *DBEngineServer) tiktoeMatchesHandler(w http.ResponseWriter, r *http.Request) {
	tenantID, err := s.tenantID(r)
	if err != nil {
		http.Error(w, err.Error(), http.StatusBadRequest)
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
	match, err := createTiktoeMatch(r.Context(), s.store, tenantID, req)
	if err != nil {
		http.Error(w, err.Error(), http.StatusBadRequest)
		return
	}
	writeJSON(w, http.StatusCreated, match)
}

func (s *DBEngineServer) tiktoeMatchByIDHandler(w http.ResponseWriter, r *http.Request) {
	tenantID, err := s.tenantID(r)
	if err != nil {
		http.Error(w, err.Error(), http.StatusBadRequest)
		return
	}
	path := strings.TrimPrefix(r.URL.Path, "/v1/tiktoe/matches/")
	parts := strings.Split(strings.Trim(path, "/"), "/")
	if len(parts) == 0 || parts[0] == "" {
		http.Error(w, "match id is required", http.StatusBadRequest)
		return
	}
	matchID := parts[0]

	if len(parts) == 1 && r.Method == http.MethodGet {
		match, err := loadTiktoeMatch(r.Context(), s.store, tenantID, matchID)
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
		var req moveRequest
		if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
			http.Error(w, "invalid json", http.StatusBadRequest)
			return
		}
		match, err := applyTiktoeMove(r.Context(), s.store, tenantID, matchID, req)
		if err != nil {
			http.Error(w, err.Error(), http.StatusBadRequest)
			return
		}
		writeJSON(w, http.StatusOK, match)
		return
	}

	if len(parts) == 2 && parts[1] == "chat" {
		if r.Method == http.MethodGet {
			limit, _ := strconv.Atoi(r.URL.Query().Get("limit"))
			items, err := listTiktoeChat(r.Context(), s.store, tenantID, matchID, limit)
			if err != nil {
				http.Error(w, err.Error(), http.StatusInternalServerError)
				return
			}
			writeJSON(w, http.StatusOK, items)
			return
		}
		if r.Method == http.MethodPost {
			var req chatRequest
			if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
				http.Error(w, "invalid json", http.StatusBadRequest)
				return
			}
			msg, err := postTiktoeChat(r.Context(), s.store, tenantID, matchID, req)
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

func (s *DBEngineServer) tiktoeMatchmakingEnqueueHandler(w http.ResponseWriter, r *http.Request) {
	tenantID, err := s.tenantID(r)
	if err != nil {
		http.Error(w, err.Error(), http.StatusBadRequest)
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
	result, err := enqueueTiktoe(r.Context(), s.store, tenantID, req)
	if err != nil {
		http.Error(w, err.Error(), http.StatusBadRequest)
		return
	}
	writeJSON(w, http.StatusOK, result)
}

func (s *DBEngineServer) tiktoeMatchmakingStatusHandler(w http.ResponseWriter, r *http.Request) {
	tenantID, err := s.tenantID(r)
	if err != nil {
		http.Error(w, err.Error(), http.StatusBadRequest)
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
	status, err := tiktoeQueueStatus(r.Context(), s.store, tenantID, userID, boardSize, winLength)
	if err != nil {
		http.Error(w, err.Error(), http.StatusInternalServerError)
		return
	}
	writeJSON(w, http.StatusOK, status)
}

func (s *DBEngineServer) tiktoePresenceHandler(w http.ResponseWriter, r *http.Request) {
	tenantID, err := s.tenantID(r)
	if err != nil {
		http.Error(w, err.Error(), http.StatusBadRequest)
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
	presence, err := upsertTiktoePresence(r.Context(), s.store, tenantID, req)
	if err != nil {
		http.Error(w, err.Error(), http.StatusBadRequest)
		return
	}
	writeJSON(w, http.StatusOK, presence)
}

func (s *DBEngineServer) tiktoePlayersHandler(w http.ResponseWriter, r *http.Request) {
	tenantID, err := s.tenantID(r)
	if err != nil {
		http.Error(w, err.Error(), http.StatusBadRequest)
		return
	}
	if r.Method != http.MethodGet {
		http.Error(w, "method not allowed", http.StatusMethodNotAllowed)
		return
	}
	limit, _ := strconv.Atoi(r.URL.Query().Get("limit"))
	items, err := listTiktoePresence(r.Context(), s.store, tenantID, r.URL.Query().Get("exclude_user_id"), r.URL.Query().Get("q"), limit)
	if err != nil {
		http.Error(w, err.Error(), http.StatusInternalServerError)
		return
	}
	writeJSON(w, http.StatusOK, items)
}
