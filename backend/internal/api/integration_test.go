package api_test

import (
	"bytes"
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
	"time"

	"github.com/gorilla/websocket"
	"hubgame/backend/internal/api"
	"hubgame/backend/internal/controller"
	"hubgame/backend/internal/database"
)

func TestGatewayIntegrationAuthIfMatchAndWebsocket(t *testing.T) {
	ctx := context.Background()
	store, err := database.OpenSQLite(ctx, "file::memory:?cache=shared")
	if err != nil {
		t.Fatalf("open store: %v", err)
	}
	defer store.Close()
	store.RegisterController(controller.SchemaController{})

	const (
		secret        = "test-secret"
		issuer        = "test-issuer"
		internalToken = "internal-token"
		adminToken    = "admin-token"
	)

	auth := controller.NewAuthController(secret, issuer)
	controllerSrv := httptest.NewServer(api.NewControllerService(auth, adminToken).Router())
	defer controllerSrv.Close()

	dbEngineSrv := httptest.NewServer(api.NewDBEngineServer(store, internalToken).Router())
	defer dbEngineSrv.Close()

	gatewaySrv := httptest.NewServer(api.NewGatewayServer(controllerSrv.URL, dbEngineSrv.URL, internalToken).Router())
	defer gatewaySrv.Close()

	token := issueToken(t, controllerSrv.URL, adminToken, "u-1", "t-1", "developer")

	createResp := requestJSON(t, http.MethodPost, gatewaySrv.URL+"/v1/entities", token, map[string]any{
		"id":   "m-1",
		"kind": "match",
		"data": map[string]any{"mode": "pvp", "status": "created"},
	}, nil)
	if createResp.StatusCode != http.StatusCreated {
		t.Fatalf("expected 201 for create, got %d", createResp.StatusCode)
	}
	createResp.Body.Close()

	conflictHeaders := map[string]string{"If-Match": "99"}
	conflictResp := requestJSON(t, http.MethodPatch, gatewaySrv.URL+"/v1/entities/m-1", token, map[string]any{
		"data": map[string]any{"mode": "pvp", "status": "running"},
	}, conflictHeaders)
	if conflictResp.StatusCode != http.StatusConflict {
		t.Fatalf("expected 409 conflict, got %d", conflictResp.StatusCode)
	}
	conflictResp.Body.Close()

	okHeaders := map[string]string{"If-Match": "1"}
	updateResp := requestJSON(t, http.MethodPatch, gatewaySrv.URL+"/v1/entities/m-1", token, map[string]any{
		"data": map[string]any{"mode": "pvp", "status": "running"},
	}, okHeaders)
	if updateResp.StatusCode != http.StatusOK {
		t.Fatalf("expected 200 update, got %d", updateResp.StatusCode)
	}
	updateResp.Body.Close()

	wsURL := strings.Replace(gatewaySrv.URL, "http://", "ws://", 1) + "/v1/events/stream?topic=room.chat"
	wsHeaders := http.Header{}
	wsHeaders.Set("Authorization", "Bearer "+token)
	wsConn, _, err := websocket.DefaultDialer.Dial(wsURL, wsHeaders)
	if err != nil {
		t.Fatalf("dial websocket: %v", err)
	}
	defer wsConn.Close()
	_ = wsConn.SetReadDeadline(time.Now().Add(3 * time.Second))

	eventResp := requestJSON(t, http.MethodPost, gatewaySrv.URL+"/v1/events", token, map[string]any{
		"topic":   "room.chat",
		"key":     "room-1",
		"type":    "chat.send",
		"payload": map[string]any{"room_id": "room-1", "message": "hello"},
	}, nil)
	if eventResp.StatusCode != http.StatusCreated {
		t.Fatalf("expected 201 event create, got %d", eventResp.StatusCode)
	}
	eventResp.Body.Close()

	_, msg, err := wsConn.ReadMessage()
	if err != nil {
		t.Fatalf("read websocket message: %v", err)
	}
	var ev database.Event
	if err := json.Unmarshal(msg, &ev); err != nil {
		t.Fatalf("unmarshal websocket event: %v", err)
	}
	if ev.Type != "chat.send" {
		t.Fatalf("expected chat.send event type, got %q", ev.Type)
	}
	if ev.TenantID != "t-1" {
		t.Fatalf("expected tenant t-1, got %q", ev.TenantID)
	}
}

func issueToken(t *testing.T, baseURL, adminToken, userID, tenantID, role string) string {
	t.Helper()
	resp := requestJSON(t, http.MethodPost, baseURL+"/v1/auth/token", "", map[string]any{
		"user_id":     userID,
		"tenant_id":   tenantID,
		"role":        role,
		"ttl_seconds": 3600,
	}, map[string]string{"X-Controller-Admin": adminToken})
	defer resp.Body.Close()
	if resp.StatusCode != http.StatusOK {
		t.Fatalf("issue token failed with %d", resp.StatusCode)
	}
	var out map[string]string
	if err := json.NewDecoder(resp.Body).Decode(&out); err != nil {
		t.Fatalf("decode token response: %v", err)
	}
	return out["token"]
}

func requestJSON(t *testing.T, method, target, bearer string, body any, headers map[string]string) *http.Response {
	t.Helper()
	var payload []byte
	var err error
	if body != nil {
		payload, err = json.Marshal(body)
		if err != nil {
			t.Fatalf("marshal request body: %v", err)
		}
	}
	req, err := http.NewRequest(method, target, bytes.NewReader(payload))
	if err != nil {
		t.Fatalf("create request: %v", err)
	}
	if body != nil {
		req.Header.Set("Content-Type", "application/json")
	}
	if bearer != "" {
		req.Header.Set("Authorization", "Bearer "+bearer)
	}
	for k, v := range headers {
		req.Header.Set(k, v)
	}
	resp, err := http.DefaultClient.Do(req)
	if err != nil {
		t.Fatalf("do request: %v", err)
	}
	return resp
}
