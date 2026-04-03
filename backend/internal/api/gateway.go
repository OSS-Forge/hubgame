package api

import (
	"context"
	"encoding/json"
	"io"
	"net/http"
	"net/url"
	"strings"
	"time"

	"github.com/gorilla/websocket"
	"hubgame/backend/internal/controller"
	"hubgame/backend/internal/controllerclient"
)

type gatewayClaimsKey struct{}

type GatewayServer struct {
	controller           *controllerclient.Client
	authorizer           *controller.Authorizer
	dbEngineURL          string
	internalKey          string
	controllerAdminToken string
	devAuthEnabled       bool
	http                 *http.Client
}

func NewGatewayServer(controllerURL, dbEngineURL, internalKey, controllerAdminToken string, devAuthEnabled bool) *GatewayServer {
	return &GatewayServer{
		controller:           controllerclient.New(controllerURL),
		authorizer:           controller.NewAuthorizer(),
		dbEngineURL:          strings.TrimRight(dbEngineURL, "/"),
		internalKey:          internalKey,
		controllerAdminToken: controllerAdminToken,
		devAuthEnabled:       devAuthEnabled,
		http:                 &http.Client{Timeout: 10 * time.Second},
	}
}

func (g *GatewayServer) Router() http.Handler {
	mux := http.NewServeMux()
	mux.HandleFunc("/healthz", func(w http.ResponseWriter, _ *http.Request) {
		writeJSON(w, http.StatusOK, map[string]string{"status": "ok", "service": "gateway"})
	})
	mux.HandleFunc("/v1/auth/dev-token", g.devTokenHandler)
	mux.Handle("/v1/leaderboard", g.requireAuth(http.HandlerFunc(g.leaderboardHandler)))
	mux.Handle("/v1/leaderboard/users", g.requireAuth(http.HandlerFunc(g.leaderboardUsersHandler)))
	mux.Handle("/v1/leaderboard/scores", g.requireAuth(http.HandlerFunc(g.leaderboardScoresHandler)))
	mux.Handle("/v1/tiktoe/matches", g.requireAuth(http.HandlerFunc(g.tiktoeMatchesHandler)))
	mux.Handle("/v1/tiktoe/matches/", g.requireAuth(http.HandlerFunc(g.tiktoeMatchByIDHandler)))
	mux.Handle("/v1/tiktoe/matchmaking/enqueue", g.requireAuth(http.HandlerFunc(g.tiktoeMatchmakingEnqueueHandler)))
	mux.Handle("/v1/tiktoe/matchmaking/status", g.requireAuth(http.HandlerFunc(g.tiktoeMatchmakingStatusHandler)))
	mux.Handle("/v1/tiktoe/presence", g.requireAuth(http.HandlerFunc(g.tiktoePresenceHandler)))
	mux.Handle("/v1/tiktoe/players", g.requireAuth(http.HandlerFunc(g.tiktoePlayersHandler)))
	mux.Handle("/v1/events/stream", g.requireAuth(g.requireAction(controller.ActionStreamRead, http.HandlerFunc(g.streamProxy))))
	mux.Handle("/v1/entities", g.requireAuth(http.HandlerFunc(g.entitiesProxy)))
	mux.Handle("/v1/entities/", g.requireAuth(http.HandlerFunc(g.entityByIDProxy)))
	mux.Handle("/v1/events", g.requireAuth(http.HandlerFunc(g.eventsProxy)))
	return withCORS(withRequestDebug("gateway", mux))
}

func (g *GatewayServer) devTokenHandler(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodPost {
		http.Error(w, "method not allowed", http.StatusMethodNotAllowed)
		return
	}
	if !g.devAuthEnabled {
		http.Error(w, "dev auth is disabled", http.StatusNotFound)
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
	if req.UserID == "" {
		req.UserID = "web-dev-user"
	}
	if req.TenantID == "" {
		req.TenantID = "hubgame-dev"
	}
	if req.Role == "" {
		req.Role = "developer"
	}
	if req.TTL <= 0 {
		req.TTL = 3600 * 6
	}

	token, err := g.controller.IssueToken(r.Context(), g.controllerAdminToken, req.UserID, req.TenantID, req.Role, req.TTL)
	if err != nil {
		http.Error(w, "failed to issue token", http.StatusBadGateway)
		return
	}
	writeJSON(w, http.StatusOK, map[string]string{"token": token})
}

func (g *GatewayServer) requireAuth(next http.Handler) http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		token := strings.TrimSpace(r.URL.Query().Get("access_token"))
		if token == "" {
			authHeader := r.Header.Get("Authorization")
			parts := strings.SplitN(authHeader, " ", 2)
			if len(parts) != 2 || !strings.EqualFold(parts[0], "Bearer") {
				debugf("gateway auth failed path=%s reason=missing_bearer", r.URL.Path)
				http.Error(w, "missing bearer token", http.StatusUnauthorized)
				return
			}
			token = strings.TrimSpace(parts[1])
		}
		claims, err := g.controller.VerifyToken(r.Context(), token)
		if err != nil {
			debugf("gateway auth failed path=%s reason=invalid_token err=%v", r.URL.Path, err)
			http.Error(w, "invalid token", http.StatusUnauthorized)
			return
		}
		r = r.WithContext(withGatewayClaims(r.Context(), claims))
		next.ServeHTTP(w, r)
	})
}

func (g *GatewayServer) requireAction(action string, next http.Handler) http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		claims, _ := gatewayClaimsFromContext(r.Context())
		if err := g.authorizer.Enforce(claims, action); err != nil {
			http.Error(w, err.Error(), http.StatusForbidden)
			return
		}
		next.ServeHTTP(w, r)
	})
}

func (g *GatewayServer) entitiesProxy(w http.ResponseWriter, r *http.Request) {
	claims, _ := gatewayClaimsFromContext(r.Context())
	if r.Method == http.MethodGet {
		if err := g.authorizer.Enforce(claims, controller.ActionEntityRead); err != nil {
			http.Error(w, err.Error(), http.StatusForbidden)
			return
		}
	} else if r.Method == http.MethodPost {
		if err := g.authorizer.Enforce(claims, controller.ActionEntityWrite); err != nil {
			http.Error(w, err.Error(), http.StatusForbidden)
			return
		}
	}
	g.proxyHTTP(w, r, claims.TenantID)
}

func (g *GatewayServer) entityByIDProxy(w http.ResponseWriter, r *http.Request) {
	claims, _ := gatewayClaimsFromContext(r.Context())
	switch r.Method {
	case http.MethodGet:
		if err := g.authorizer.Enforce(claims, controller.ActionEntityRead); err != nil {
			http.Error(w, err.Error(), http.StatusForbidden)
			return
		}
	case http.MethodPatch:
		if err := g.authorizer.Enforce(claims, controller.ActionEntityWrite); err != nil {
			http.Error(w, err.Error(), http.StatusForbidden)
			return
		}
	case http.MethodDelete:
		if err := g.authorizer.Enforce(claims, controller.ActionEntityDelete); err != nil {
			http.Error(w, err.Error(), http.StatusForbidden)
			return
		}
	}
	g.proxyHTTP(w, r, claims.TenantID)
}

func (g *GatewayServer) eventsProxy(w http.ResponseWriter, r *http.Request) {
	claims, _ := gatewayClaimsFromContext(r.Context())
	if r.Method == http.MethodGet {
		if err := g.authorizer.Enforce(claims, controller.ActionEventRead); err != nil {
			http.Error(w, err.Error(), http.StatusForbidden)
			return
		}
	} else if r.Method == http.MethodPost {
		if err := g.authorizer.Enforce(claims, controller.ActionEventWrite); err != nil {
			http.Error(w, err.Error(), http.StatusForbidden)
			return
		}
	}
	g.proxyHTTP(w, r, claims.TenantID)
}

func (g *GatewayServer) leaderboardHandler(w http.ResponseWriter, r *http.Request) {
	claims, _ := gatewayClaimsFromContext(r.Context())
	if err := g.authorizer.Enforce(claims, controller.ActionLeaderboardRead); err != nil {
		http.Error(w, err.Error(), http.StatusForbidden)
		return
	}
	g.proxyHTTP(w, r, claims.TenantID)
}

func (g *GatewayServer) leaderboardUsersHandler(w http.ResponseWriter, r *http.Request) {
	claims, _ := gatewayClaimsFromContext(r.Context())
	if err := g.authorizer.Enforce(claims, controller.ActionLeaderboardWrite); err != nil {
		http.Error(w, err.Error(), http.StatusForbidden)
		return
	}
	g.proxyHTTP(w, r, claims.TenantID)
}

func (g *GatewayServer) leaderboardScoresHandler(w http.ResponseWriter, r *http.Request) {
	claims, _ := gatewayClaimsFromContext(r.Context())
	if r.Method == http.MethodGet {
		if err := g.authorizer.Enforce(claims, controller.ActionLeaderboardRead); err != nil {
			http.Error(w, err.Error(), http.StatusForbidden)
			return
		}
	} else {
		if err := g.authorizer.Enforce(claims, controller.ActionLeaderboardWrite); err != nil {
			http.Error(w, err.Error(), http.StatusForbidden)
			return
		}
	}
	g.proxyHTTP(w, r, claims.TenantID)
}

func (g *GatewayServer) tiktoeMatchesHandler(w http.ResponseWriter, r *http.Request) {
	claims, _ := gatewayClaimsFromContext(r.Context())
	if r.Method == http.MethodPost {
		if err := g.authorizer.Enforce(claims, controller.ActionEntityWrite); err != nil {
			http.Error(w, err.Error(), http.StatusForbidden)
			return
		}
	} else if r.Method == http.MethodGet {
		if err := g.authorizer.Enforce(claims, controller.ActionEntityRead); err != nil {
			http.Error(w, err.Error(), http.StatusForbidden)
			return
		}
	}
	g.proxyHTTP(w, r, claims.TenantID)
}

func (g *GatewayServer) tiktoeMatchByIDHandler(w http.ResponseWriter, r *http.Request) {
	claims, _ := gatewayClaimsFromContext(r.Context())
	switch r.Method {
	case http.MethodGet:
		if err := g.authorizer.Enforce(claims, controller.ActionEntityRead); err != nil {
			http.Error(w, err.Error(), http.StatusForbidden)
			return
		}
	case http.MethodPost:
		if strings.HasSuffix(r.URL.Path, "/chat") {
			if err := g.authorizer.Enforce(claims, controller.ActionEventWrite); err != nil {
				http.Error(w, err.Error(), http.StatusForbidden)
				return
			}
		} else {
			if err := g.authorizer.Enforce(claims, controller.ActionEntityWrite); err != nil {
				http.Error(w, err.Error(), http.StatusForbidden)
				return
			}
		}
	}
	g.proxyHTTP(w, r, claims.TenantID)
}

func (g *GatewayServer) tiktoeMatchmakingEnqueueHandler(w http.ResponseWriter, r *http.Request) {
	claims, _ := gatewayClaimsFromContext(r.Context())
	if err := g.authorizer.Enforce(claims, controller.ActionEntityWrite); err != nil {
		http.Error(w, err.Error(), http.StatusForbidden)
		return
	}
	g.proxyHTTP(w, r, claims.TenantID)
}

func (g *GatewayServer) tiktoeMatchmakingStatusHandler(w http.ResponseWriter, r *http.Request) {
	claims, _ := gatewayClaimsFromContext(r.Context())
	if err := g.authorizer.Enforce(claims, controller.ActionEntityRead); err != nil {
		http.Error(w, err.Error(), http.StatusForbidden)
		return
	}
	g.proxyHTTP(w, r, claims.TenantID)
}

func (g *GatewayServer) tiktoePresenceHandler(w http.ResponseWriter, r *http.Request) {
	claims, _ := gatewayClaimsFromContext(r.Context())
	if err := g.authorizer.Enforce(claims, controller.ActionEntityWrite); err != nil {
		http.Error(w, err.Error(), http.StatusForbidden)
		return
	}
	g.proxyHTTP(w, r, claims.TenantID)
}

func (g *GatewayServer) tiktoePlayersHandler(w http.ResponseWriter, r *http.Request) {
	claims, _ := gatewayClaimsFromContext(r.Context())
	if err := g.authorizer.Enforce(claims, controller.ActionEntityRead); err != nil {
		http.Error(w, err.Error(), http.StatusForbidden)
		return
	}
	g.proxyHTTP(w, r, claims.TenantID)
}

func (g *GatewayServer) proxyHTTP(w http.ResponseWriter, r *http.Request, tenantID string) {
	target := g.dbEngineURL + r.URL.Path
	if r.URL.RawQuery != "" {
		target += "?" + r.URL.RawQuery
	}
	debugf("gateway proxy start method=%s path=%s tenant=%s target=%s", r.Method, r.URL.Path, tenantID, target)
	req, err := http.NewRequestWithContext(r.Context(), r.Method, target, r.Body)
	if err != nil {
		http.Error(w, "failed to build upstream request", http.StatusInternalServerError)
		return
	}
	req.Header.Set("Content-Type", r.Header.Get("Content-Type"))
	req.Header.Set("If-Match", r.Header.Get("If-Match"))
	req.Header.Set("X-Internal-Token", g.internalKey)
	req.Header.Set("X-Tenant-ID", tenantID)

	resp, err := g.http.Do(req)
	if err != nil {
		debugf("gateway proxy upstream error method=%s path=%s tenant=%s err=%v", r.Method, r.URL.Path, tenantID, err)
		http.Error(w, "db-engine unavailable", http.StatusBadGateway)
		return
	}
	defer resp.Body.Close()
	debugf("gateway proxy done method=%s path=%s tenant=%s status=%d", r.Method, r.URL.Path, tenantID, resp.StatusCode)

	w.Header().Set("Content-Type", resp.Header.Get("Content-Type"))
	w.WriteHeader(resp.StatusCode)
	_, _ = io.Copy(w, resp.Body)
}

func (g *GatewayServer) streamProxy(w http.ResponseWriter, r *http.Request) {
	claims, _ := gatewayClaimsFromContext(r.Context())
	clientConn, err := websocket.Upgrade(w, r, nil, 1024, 1024)
	if err != nil {
		return
	}
	defer clientConn.Close()

	upstreamURL, err := url.Parse(g.dbEngineURL)
	if err != nil {
		return
	}
	if upstreamURL.Scheme == "https" {
		upstreamURL.Scheme = "wss"
	} else {
		upstreamURL.Scheme = "ws"
	}
	upstreamURL.Path = "/v1/events/stream"
	upstreamURL.RawQuery = r.URL.RawQuery

	headers := http.Header{}
	headers.Set("X-Internal-Token", g.internalKey)
	headers.Set("X-Tenant-ID", claims.TenantID)
	upstreamConn, _, err := websocket.DefaultDialer.Dial(upstreamURL.String(), headers)
	if err != nil {
		return
	}
	defer upstreamConn.Close()

	errCh := make(chan error, 2)
	go bridgeWebsocket(clientConn, upstreamConn, errCh)
	go bridgeWebsocket(upstreamConn, clientConn, errCh)
	<-errCh
}

func bridgeWebsocket(src, dst *websocket.Conn, errCh chan<- error) {
	for {
		mt, msg, err := src.ReadMessage()
		if err != nil {
			errCh <- err
			return
		}
		if err := dst.WriteMessage(mt, msg); err != nil {
			errCh <- err
			return
		}
	}
}

func withGatewayClaims(ctx context.Context, claims *controller.Claims) context.Context {
	return context.WithValue(ctx, gatewayClaimsKey{}, claims)
}

func gatewayClaimsFromContext(ctx context.Context) (*controller.Claims, bool) {
	claims, ok := ctx.Value(gatewayClaimsKey{}).(*controller.Claims)
	return claims, ok
}
