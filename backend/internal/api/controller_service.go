package api

import (
	"encoding/json"
	"net/http"
	"time"

	"hubgame/backend/internal/controller"
)

type ControllerService struct {
	auth       *controller.AuthController
	adminToken string
}

func NewControllerService(auth *controller.AuthController, adminToken string) *ControllerService {
	return &ControllerService{auth: auth, adminToken: adminToken}
}

func (s *ControllerService) Router() http.Handler {
	mux := http.NewServeMux()
	mux.HandleFunc("/healthz", func(w http.ResponseWriter, _ *http.Request) {
		writeJSON(w, http.StatusOK, map[string]string{"status": "ok", "service": "controller"})
	})
	mux.HandleFunc("/v1/auth/token", s.issueToken)
	mux.HandleFunc("/v1/auth/verify", s.verifyToken)
	return withCORS(withRequestDebug("controller", mux))
}

func (s *ControllerService) issueToken(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodPost {
		http.Error(w, "method not allowed", http.StatusMethodNotAllowed)
		return
	}
	if r.Header.Get("X-Controller-Admin") != s.adminToken {
		http.Error(w, "forbidden", http.StatusForbidden)
		return
	}
	var req struct {
		UserID   string `json:"user_id"`
		TenantID string `json:"tenant_id"`
		Role     string `json:"role"`
		TTL      int64  `json:"ttl_seconds"`
	}
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		http.Error(w, "invalid json", http.StatusBadRequest)
		return
	}
	if req.TTL <= 0 {
		req.TTL = 3600
	}
	token, err := s.auth.IssueToken(req.UserID, req.TenantID, req.Role, time.Duration(req.TTL)*time.Second)
	if err != nil {
		http.Error(w, "failed to issue token", http.StatusInternalServerError)
		return
	}
	writeJSON(w, http.StatusOK, map[string]string{"token": token})
}

func (s *ControllerService) verifyToken(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodPost {
		http.Error(w, "method not allowed", http.StatusMethodNotAllowed)
		return
	}
	var req struct {
		Token string `json:"token"`
	}
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		http.Error(w, "invalid json", http.StatusBadRequest)
		return
	}
	claims, err := s.auth.ParseToken(req.Token)
	if err != nil {
		http.Error(w, "invalid token", http.StatusUnauthorized)
		return
	}
	writeJSON(w, http.StatusOK, map[string]any{"claims": claims})
}
