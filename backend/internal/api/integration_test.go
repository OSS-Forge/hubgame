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
	controllerSrv, _, gatewaySrv, cleanup := setupIntegrationStack(t)
	defer cleanup()

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

func TestGatewayIntegrationRBACAndUnauthorizedWebsocket(t *testing.T) {
	controllerSrv, _, gatewaySrv, cleanup := setupIntegrationStack(t)
	defer cleanup()

	playerToken := issueToken(t, controllerSrv.URL, adminToken, "u-2", "t-1", "player")
	devToken := issueToken(t, controllerSrv.URL, adminToken, "u-3", "t-1", "developer")

	createResp := requestJSON(t, http.MethodPost, gatewaySrv.URL+"/v1/entities", devToken, map[string]any{
		"id":   "room-1",
		"kind": "room",
		"data": map[string]any{"name": "Main Room"},
	}, nil)
	if createResp.StatusCode != http.StatusCreated {
		t.Fatalf("expected 201 create for developer, got %d", createResp.StatusCode)
	}
	createResp.Body.Close()

	deleteResp := requestJSON(t, http.MethodDelete, gatewaySrv.URL+"/v1/entities/room-1", playerToken, nil, nil)
	if deleteResp.StatusCode != http.StatusForbidden {
		t.Fatalf("expected 403 for player delete, got %d", deleteResp.StatusCode)
	}
	deleteResp.Body.Close()

	wsURL := strings.Replace(gatewaySrv.URL, "http://", "ws://", 1) + "/v1/events/stream?topic=room.chat"

	_, unauthorizedResp, err := websocket.DefaultDialer.Dial(wsURL, nil)
	if err == nil {
		t.Fatalf("expected websocket dial failure without auth")
	}
	if unauthorizedResp == nil {
		t.Fatalf("expected HTTP response for unauthorized websocket handshake")
	}
	if unauthorizedResp.StatusCode != http.StatusUnauthorized {
		t.Fatalf("expected 401 websocket handshake without auth, got %d", unauthorizedResp.StatusCode)
	}

	wsHeaders := http.Header{}
	wsHeaders.Set("Authorization", "Bearer "+playerToken)
	wsConn, _, err := websocket.DefaultDialer.Dial(wsURL, wsHeaders)
	if err != nil {
		t.Fatalf("expected player websocket stream access, got dial error: %v", err)
	}
	_ = wsConn.Close()
}

func TestGatewayIntegrationLeaderboardGlobalAndGame(t *testing.T) {
	controllerSrv, _, gatewaySrv, cleanup := setupIntegrationStack(t)
	defer cleanup()

	devToken := issueToken(t, controllerSrv.URL, adminToken, "dev-1", "t-1", "developer")

	u1 := requestJSON(t, http.MethodPost, gatewaySrv.URL+"/v1/leaderboard/users", devToken, map[string]any{
		"user_id":      "u-1",
		"display_name": "Ada",
		"rank_title":   "Bronze",
	}, nil)
	if u1.StatusCode != http.StatusOK {
		t.Fatalf("expected 200 create leaderboard user, got %d", u1.StatusCode)
	}
	u1.Body.Close()

	u2 := requestJSON(t, http.MethodPost, gatewaySrv.URL+"/v1/leaderboard/users", devToken, map[string]any{
		"user_id":      "u-2",
		"display_name": "Turing",
		"rank_title":   "Silver",
	}, nil)
	if u2.StatusCode != http.StatusOK {
		t.Fatalf("expected 200 create leaderboard user 2, got %d", u2.StatusCode)
	}
	u2.Body.Close()

	s1 := requestJSON(t, http.MethodPost, gatewaySrv.URL+"/v1/leaderboard/scores", devToken, map[string]any{
		"game_id":        "tik-toe",
		"user_id":        "u-1",
		"score_delta":    12,
		"hubcoins_delta": 30,
	}, nil)
	if s1.StatusCode != http.StatusCreated {
		t.Fatalf("expected 201 score submit, got %d", s1.StatusCode)
	}
	s1.Body.Close()

	s2 := requestJSON(t, http.MethodPost, gatewaySrv.URL+"/v1/leaderboard/scores", devToken, map[string]any{
		"game_id":        "tik-toe",
		"user_id":        "u-2",
		"score_delta":    20,
		"hubcoins_delta": 10,
	}, nil)
	if s2.StatusCode != http.StatusCreated {
		t.Fatalf("expected 201 score submit 2, got %d", s2.StatusCode)
	}
	s2.Body.Close()

	gameLB := requestJSON(t, http.MethodGet, gatewaySrv.URL+"/v1/leaderboard?scope=game&game_id=tik-toe&limit=5", devToken, nil, nil)
	if gameLB.StatusCode != http.StatusOK {
		t.Fatalf("expected 200 game leaderboard, got %d", gameLB.StatusCode)
	}
	var gamePayload struct {
		Items []struct {
			Rank        int    `json:"rank"`
			UserID      string `json:"user_id"`
			DisplayName string `json:"display_name"`
			Score       int    `json:"score"`
		} `json:"items"`
	}
	if err := json.NewDecoder(gameLB.Body).Decode(&gamePayload); err != nil {
		t.Fatalf("decode game leaderboard: %v", err)
	}
	gameLB.Body.Close()
	if len(gamePayload.Items) < 2 {
		t.Fatalf("expected at least 2 leaderboard rows, got %d", len(gamePayload.Items))
	}
	if gamePayload.Items[0].UserID != "u-2" {
		t.Fatalf("expected u-2 top rank, got %s", gamePayload.Items[0].UserID)
	}

	globalLB := requestJSON(t, http.MethodGet, gatewaySrv.URL+"/v1/leaderboard?scope=global&limit=5", devToken, nil, nil)
	if globalLB.StatusCode != http.StatusOK {
		t.Fatalf("expected 200 global leaderboard, got %d", globalLB.StatusCode)
	}
	var globalPayload struct {
		Items []struct {
			UserID      string `json:"user_id"`
			GamesPlayed int    `json:"games_played"`
			Hubcoins    int    `json:"hubcoins"`
		} `json:"items"`
	}
	if err := json.NewDecoder(globalLB.Body).Decode(&globalPayload); err != nil {
		t.Fatalf("decode global leaderboard: %v", err)
	}
	globalLB.Body.Close()
	if len(globalPayload.Items) == 0 {
		t.Fatalf("expected global leaderboard rows")
	}
	if globalPayload.Items[0].GamesPlayed == 0 {
		t.Fatalf("expected non-zero games played for top user")
	}
}

func TestGatewayIntegrationTiktoeOnlineAndChat(t *testing.T) {
	controllerSrv, _, gatewaySrv, cleanup := setupIntegrationStack(t)
	defer cleanup()

	devToken := issueToken(t, controllerSrv.URL, adminToken, "dev-1", "t-1", "developer")

	first := requestJSON(t, http.MethodPost, gatewaySrv.URL+"/v1/tiktoe/matchmaking/enqueue", devToken, map[string]any{
		"user_id":      "p1",
		"display_name": "Player One",
		"board_size":   3,
		"win_length":   3,
	}, nil)
	if first.StatusCode != http.StatusOK {
		t.Fatalf("expected 200 first enqueue, got %d", first.StatusCode)
	}
	first.Body.Close()

	second := requestJSON(t, http.MethodPost, gatewaySrv.URL+"/v1/tiktoe/matchmaking/enqueue", devToken, map[string]any{
		"user_id":      "p2",
		"display_name": "Player Two",
		"board_size":   3,
		"win_length":   3,
	}, nil)
	if second.StatusCode != http.StatusOK {
		t.Fatalf("expected 200 second enqueue, got %d", second.StatusCode)
	}
	var matched struct {
		Status string `json:"status"`
		Match  struct {
			ID string `json:"id"`
		} `json:"match"`
	}
	if err := json.NewDecoder(second.Body).Decode(&matched); err != nil {
		t.Fatalf("decode matched response: %v", err)
	}
	second.Body.Close()
	if matched.Status != "matched" || matched.Match.ID == "" {
		t.Fatalf("expected matched status with match id")
	}
	moveUser := "p1"
	if matched.Match.ID != "" {
		// current turn starts with player_x in server state
		var matchState struct {
			PlayerX string `json:"player_x"`
		}
		matchResp := requestJSON(t, http.MethodGet, gatewaySrv.URL+"/v1/tiktoe/matches/"+matched.Match.ID, devToken, nil, nil)
		if matchResp.StatusCode == http.StatusOK {
			if err := json.NewDecoder(matchResp.Body).Decode(&matchState); err == nil && matchState.PlayerX != "" {
				moveUser = matchState.PlayerX
			}
		}
		matchResp.Body.Close()
	}

	move := requestJSON(t, http.MethodPost, gatewaySrv.URL+"/v1/tiktoe/matches/"+matched.Match.ID+"/moves", devToken, map[string]any{
		"user_id": moveUser,
		"row":     0,
		"col":     0,
	}, nil)
	if move.StatusCode != http.StatusOK {
		t.Fatalf("expected 200 move, got %d", move.StatusCode)
	}
	move.Body.Close()

	chat := requestJSON(t, http.MethodPost, gatewaySrv.URL+"/v1/tiktoe/matches/"+matched.Match.ID+"/chat", devToken, map[string]any{
		"user_id": moveUser,
		"message": "gg",
		"emoji":   "🔥",
	}, nil)
	if chat.StatusCode != http.StatusCreated {
		t.Fatalf("expected 201 chat, got %d", chat.StatusCode)
	}
	chat.Body.Close()

	chatList := requestJSON(t, http.MethodGet, gatewaySrv.URL+"/v1/tiktoe/matches/"+matched.Match.ID+"/chat?limit=10", devToken, nil, nil)
	if chatList.StatusCode != http.StatusOK {
		t.Fatalf("expected 200 chat list, got %d", chatList.StatusCode)
	}
	var msgs []map[string]any
	if err := json.NewDecoder(chatList.Body).Decode(&msgs); err != nil {
		t.Fatalf("decode chat list: %v", err)
	}
	chatList.Body.Close()
	if len(msgs) == 0 {
		t.Fatalf("expected at least one chat message")
	}
}

func TestGatewayIntegrationTiktoeMatchmakingDistinctUsers(t *testing.T) {
	controllerSrv, _, gatewaySrv, cleanup := setupIntegrationStack(t)
	defer cleanup()

	devToken := issueToken(t, controllerSrv.URL, adminToken, "dev-1", "t-1", "developer")

	one := requestJSON(t, http.MethodPost, gatewaySrv.URL+"/v1/tiktoe/matchmaking/enqueue", devToken, map[string]any{
		"user_id":      "player1",
		"display_name": "Player One",
		"board_size":   3,
		"win_length":   3,
	}, nil)
	if one.StatusCode != http.StatusOK {
		t.Fatalf("expected 200 first enqueue, got %d", one.StatusCode)
	}
	one.Body.Close()

	sameUser := requestJSON(t, http.MethodPost, gatewaySrv.URL+"/v1/tiktoe/matchmaking/enqueue", devToken, map[string]any{
		"user_id":      "PLAYER1",
		"display_name": "Player One Uppercase",
		"board_size":   3,
		"win_length":   3,
	}, nil)
	if sameUser.StatusCode != http.StatusOK {
		t.Fatalf("expected 200 same-user enqueue, got %d", sameUser.StatusCode)
	}
	var samePayload map[string]any
	if err := json.NewDecoder(sameUser.Body).Decode(&samePayload); err != nil {
		t.Fatalf("decode same-user enqueue: %v", err)
	}
	sameUser.Body.Close()
	if samePayload["status"] != "queued" {
		t.Fatalf("expected queued status for same user, got %v", samePayload["status"])
	}

	second := requestJSON(t, http.MethodPost, gatewaySrv.URL+"/v1/tiktoe/matchmaking/enqueue", devToken, map[string]any{
		"user_id":      "player2",
		"display_name": "Player Two",
		"board_size":   3,
		"win_length":   3,
	}, nil)
	if second.StatusCode != http.StatusOK {
		t.Fatalf("expected 200 second user enqueue, got %d", second.StatusCode)
	}
	var matched struct {
		Status string `json:"status"`
		Match  struct {
			ID      string `json:"id"`
			PlayerX string `json:"player_x"`
			PlayerO string `json:"player_o"`
		} `json:"match"`
	}
	if err := json.NewDecoder(second.Body).Decode(&matched); err != nil {
		t.Fatalf("decode second enqueue: %v", err)
	}
	second.Body.Close()
	if matched.Status != "matched" || matched.Match.ID == "" {
		t.Fatalf("expected matched status with match id")
	}
	if matched.Match.PlayerX != "player2" || matched.Match.PlayerO != "player1" {
		t.Fatalf("expected requester to match distinct queued user, got x=%q o=%q", matched.Match.PlayerX, matched.Match.PlayerO)
	}
}

func TestGatewayIntegrationTiktoeMatchmakingReenqueueAfterMatch(t *testing.T) {
	controllerSrv, _, gatewaySrv, cleanup := setupIntegrationStack(t)
	defer cleanup()

	devToken := issueToken(t, controllerSrv.URL, adminToken, "dev-1", "t-1", "developer")

	for _, user := range []string{"p1", "p2"} {
		resp := requestJSON(t, http.MethodPost, gatewaySrv.URL+"/v1/tiktoe/matchmaking/enqueue", devToken, map[string]any{
			"user_id":      user,
			"display_name": user,
			"board_size":   3,
			"win_length":   3,
		}, nil)
		if resp.StatusCode != http.StatusOK {
			t.Fatalf("expected 200 enqueue for %s, got %d", user, resp.StatusCode)
		}
		resp.Body.Close()
	}

	reenqueue := requestJSON(t, http.MethodPost, gatewaySrv.URL+"/v1/tiktoe/matchmaking/enqueue", devToken, map[string]any{
		"user_id":      "p1",
		"display_name": "p1",
		"board_size":   3,
		"win_length":   3,
	}, nil)
	if reenqueue.StatusCode != http.StatusOK {
		t.Fatalf("expected 200 re-enqueue for p1, got %d", reenqueue.StatusCode)
	}
	reenqueue.Body.Close()

	secondRound := requestJSON(t, http.MethodPost, gatewaySrv.URL+"/v1/tiktoe/matchmaking/enqueue", devToken, map[string]any{
		"user_id":      "p3",
		"display_name": "p3",
		"board_size":   3,
		"win_length":   3,
	}, nil)
	if secondRound.StatusCode != http.StatusOK {
		t.Fatalf("expected 200 second round enqueue, got %d", secondRound.StatusCode)
	}
	var payload struct {
		Status string `json:"status"`
		Match  struct {
			ID string `json:"id"`
		} `json:"match"`
	}
	if err := json.NewDecoder(secondRound.Body).Decode(&payload); err != nil {
		t.Fatalf("decode second round payload: %v", err)
	}
	secondRound.Body.Close()
	if payload.Status != "matched" || payload.Match.ID == "" {
		t.Fatalf("expected matched status after re-enqueue, got status=%q id=%q", payload.Status, payload.Match.ID)
	}
}

func TestGatewayIntegrationTiktoePresenceListing(t *testing.T) {
	controllerSrv, _, gatewaySrv, cleanup := setupIntegrationStack(t)
	defer cleanup()

	devToken := issueToken(t, controllerSrv.URL, adminToken, "dev-1", "t-1", "developer")

	for _, payload := range []map[string]any{
		{"user_id": "ada", "display_name": "Ada", "available": true},
		{"user_id": "grace", "display_name": "Grace", "available": true},
		{"user_id": "linus", "display_name": "Linus", "available": false},
	} {
		resp := requestJSON(t, http.MethodPost, gatewaySrv.URL+"/v1/tiktoe/presence", devToken, payload, nil)
		if resp.StatusCode != http.StatusOK {
			t.Fatalf("expected 200 presence upsert, got %d", resp.StatusCode)
		}
		resp.Body.Close()
	}

	listResp := requestJSON(t, http.MethodGet, gatewaySrv.URL+"/v1/tiktoe/players?exclude_user_id=ada&q=gr&limit=10", devToken, nil, nil)
	if listResp.StatusCode != http.StatusOK {
		t.Fatalf("expected 200 player list, got %d", listResp.StatusCode)
	}
	var players []struct {
		UserID      string `json:"user_id"`
		DisplayName string `json:"display_name"`
		Available   bool   `json:"available"`
	}
	if err := json.NewDecoder(listResp.Body).Decode(&players); err != nil {
		t.Fatalf("decode player list: %v", err)
	}
	listResp.Body.Close()
	if len(players) != 1 {
		t.Fatalf("expected 1 discoverable player, got %d", len(players))
	}
	if players[0].UserID != "grace" {
		t.Fatalf("expected grace in player list, got %q", players[0].UserID)
	}
}

func TestGatewayIntegrationTiktoeDirectUsernameMatch(t *testing.T) {
	controllerSrv, _, gatewaySrv, cleanup := setupIntegrationStack(t)
	defer cleanup()

	devToken := issueToken(t, controllerSrv.URL, adminToken, "dev-1", "t-1", "developer")

	first := requestJSON(t, http.MethodPost, gatewaySrv.URL+"/v1/tiktoe/matches", devToken, map[string]any{
		"mode":        "online",
		"board_size":  3,
		"win_length":  3,
		"player_id":   "alice",
		"opponent_id": "bob",
	}, nil)
	if first.StatusCode != http.StatusCreated {
		t.Fatalf("expected 201 first direct match create, got %d", first.StatusCode)
	}
	var m1 struct {
		ID      string `json:"id"`
		PlayerX string `json:"player_x"`
		PlayerO string `json:"player_o"`
	}
	if err := json.NewDecoder(first.Body).Decode(&m1); err != nil {
		t.Fatalf("decode direct match first: %v", err)
	}
	first.Body.Close()

	second := requestJSON(t, http.MethodPost, gatewaySrv.URL+"/v1/tiktoe/matches", devToken, map[string]any{
		"mode":        "online",
		"board_size":  3,
		"win_length":  3,
		"player_id":   "bob",
		"opponent_id": "alice",
	}, nil)
	if second.StatusCode != http.StatusCreated {
		t.Fatalf("expected 201 second direct match create, got %d", second.StatusCode)
	}
	var m2 struct {
		ID string `json:"id"`
	}
	if err := json.NewDecoder(second.Body).Decode(&m2); err != nil {
		t.Fatalf("decode direct match second: %v", err)
	}
	second.Body.Close()

	if m1.ID == "" || m2.ID == "" {
		t.Fatalf("expected non-empty direct match IDs")
	}
	if m1.ID != m2.ID {
		t.Fatalf("expected same direct match id for alice/bob challenge pair")
	}
}

func setupIntegrationStack(t *testing.T) (controllerSrv, dbEngineSrv, gatewaySrv *httptest.Server, cleanup func()) {
	t.Helper()

	ctx := context.Background()
	store, err := database.OpenSQLite(ctx, "file::memory:?cache=shared")
	if err != nil {
		t.Fatalf("open store: %v", err)
	}
	store.RegisterController(controller.SchemaController{})

	auth := controller.NewAuthController(secret, issuer)
	controllerSrv = httptest.NewServer(api.NewControllerService(auth, adminToken).Router())
	dbEngineSrv = httptest.NewServer(api.NewDBEngineServer(store, internalToken).Router())
	gatewaySrv = httptest.NewServer(api.NewGatewayServer(controllerSrv.URL, dbEngineSrv.URL, internalToken, adminToken, true).Router())

	cleanup = func() {
		gatewaySrv.Close()
		dbEngineSrv.Close()
		controllerSrv.Close()
		_ = store.Close()
	}
	return controllerSrv, dbEngineSrv, gatewaySrv, cleanup
}

const (
	secret        = "test-secret"
	issuer        = "test-issuer"
	internalToken = "internal-token"
	adminToken    = "admin-token"
)

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
