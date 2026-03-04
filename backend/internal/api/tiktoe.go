package api

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"sort"
	"strings"
	"time"

	"hubgame/backend/internal/database"
)

const (
	tiktoeMatchKind = "tiktoe_match"
	tiktoeQueueKind = "tiktoe_queue"
	tiktoeChatKind  = "tiktoe_chat"
)

type tiktoeState struct {
	ID         string     `json:"id"`
	Mode       string     `json:"mode"`
	BoardSize  int        `json:"board_size"`
	WinLength  int        `json:"win_length"`
	Board      [][]string `json:"board"`
	PlayerX    string     `json:"player_x"`
	PlayerO    string     `json:"player_o"`
	Current    string     `json:"current"`
	Winner     string     `json:"winner"`
	MoveCount  int        `json:"move_count"`
	UpdatedAt  time.Time  `json:"updated_at"`
	LastAction string     `json:"last_action"`
}

type matchmakingTicket struct {
	UserID      string    `json:"user_id"`
	DisplayName string    `json:"display_name"`
	CreatedAt   time.Time `json:"created_at"`
}

type chatMessage struct {
	ID        string    `json:"id"`
	MatchID   string    `json:"match_id"`
	UserID    string    `json:"user_id"`
	Type      string    `json:"type"`
	Message   string    `json:"message,omitempty"`
	Emoji     string    `json:"emoji,omitempty"`
	CreatedAt time.Time `json:"created_at"`
}

type createMatchRequest struct {
	Mode       string `json:"mode"`
	BoardSize  int    `json:"board_size"`
	WinLength  int    `json:"win_length"`
	PlayerID   string `json:"player_id"`
	OpponentID string `json:"opponent_id"`
}

type moveRequest struct {
	UserID string `json:"user_id"`
	Row    int    `json:"row"`
	Col    int    `json:"col"`
}

type enqueueRequest struct {
	UserID      string `json:"user_id"`
	DisplayName string `json:"display_name"`
	BoardSize   int    `json:"board_size"`
	WinLength   int    `json:"win_length"`
}

type chatRequest struct {
	UserID  string `json:"user_id"`
	Message string `json:"message"`
	Emoji   string `json:"emoji"`
}

func createTiktoeMatch(ctx context.Context, store *database.Store, tenantID string, req createMatchRequest) (*tiktoeState, error) {
	req.PlayerID = normalizePlayerID(req.PlayerID)
	req.OpponentID = normalizePlayerID(req.OpponentID)
	if req.PlayerID == "" {
		return nil, errors.New("player_id is required")
	}
	boardSize, winLength := sanitizeBoard(req.BoardSize, req.WinLength)
	mode := strings.TrimSpace(req.Mode)
	if mode == "" {
		mode = "offline"
	}

	id := fmt.Sprintf("match_%d", time.Now().UnixNano())
	opponentID := req.OpponentID
	if mode == "online" && opponentID != "" {
		id = tiktoeDirectMatchID(req.PlayerID, opponentID, boardSize, winLength)
		existing, err := store.GetEntity(ctx, tenantID, id)
		if err == nil {
			var found tiktoeState
			if json.Unmarshal(existing.Data, &found) == nil {
				return &found, nil
			}
		}
		if err != nil && !errors.Is(err, database.ErrNotFound) {
			return nil, err
		}
	}
	state := &tiktoeState{
		ID:         id,
		Mode:       mode,
		BoardSize:  boardSize,
		WinLength:  winLength,
		Board:      createBoard(boardSize),
		PlayerX:    req.PlayerID,
		PlayerO:    botOrEmpty(mode),
		Current:    "X",
		Winner:     "",
		MoveCount:  0,
		UpdatedAt:  time.Now().UTC(),
		LastAction: "match.created",
	}
	if mode == "online" {
		state.PlayerO = opponentID
	}

	data, _ := json.Marshal(state)
	if err := store.InsertEntity(ctx, &database.Entity{
		ID:       state.ID,
		TenantID: tenantID,
		Kind:     tiktoeMatchKind,
		Data:     data,
	}); err != nil {
		return nil, err
	}
	_, _ = store.AppendEvent(ctx, database.Event{
		TenantID: tenantID,
		Topic:    tiktoeTopic(state.ID),
		Key:      state.ID,
		Type:     "tiktoe.match_created",
		Payload:  data,
	})
	return state, nil
}

func enqueueTiktoe(ctx context.Context, store *database.Store, tenantID string, req enqueueRequest) (map[string]any, error) {
	req.UserID = normalizePlayerID(req.UserID)
	if req.UserID == "" {
		return nil, errors.New("user_id is required")
	}
	if strings.TrimSpace(req.DisplayName) == "" {
		req.DisplayName = req.UserID
	}
	boardSize, winLength := sanitizeBoard(req.BoardSize, req.WinLength)

	queueID := tiktoeQueueID(req.UserID, boardSize, winLength)
	ticket := matchmakingTicket{UserID: req.UserID, DisplayName: req.DisplayName, CreatedAt: time.Now().UTC()}
	ticketData, _ := json.Marshal(ticket)
	err := store.InsertEntity(ctx, &database.Entity{ID: queueID, TenantID: tenantID, Kind: tiktoeQueueKind, Data: ticketData})
	if err != nil && strings.Contains(err.Error(), "UNIQUE constraint failed") {
		_ = store.UpdateEntity(ctx, &database.Entity{ID: queueID, TenantID: tenantID, Data: ticketData})
	}

	queue, err := store.ListEntities(ctx, tenantID, tiktoeQueueKind, 5000)
	if err != nil {
		return nil, err
	}
	candidates := make([]matchmakingTicket, 0)
	for _, entity := range queue {
		if !strings.HasSuffix(entity.ID, fmt.Sprintf(":%d:%d", boardSize, winLength)) {
			continue
		}
		var t matchmakingTicket
		if json.Unmarshal(entity.Data, &t) == nil {
			t.UserID = normalizePlayerID(t.UserID)
			if t.UserID == "" {
				continue
			}
			candidates = append(candidates, t)
		}
	}
	if len(candidates) < 2 {
		return map[string]any{"status": "queued", "queue_id": queueID, "waiting_for": "opponent"}, nil
	}

	sort.Slice(candidates, func(i, j int) bool {
		return candidates[i].CreatedAt.Before(candidates[j].CreatedAt)
	})

	var opponent *matchmakingTicket
	for i := range candidates {
		if candidates[i].UserID != req.UserID {
			opponent = &candidates[i]
			break
		}
	}
	if opponent == nil {
		return map[string]any{"status": "queued", "queue_id": queueID, "waiting_for": "distinct_username"}, nil
	}

	match, err := createTiktoeMatch(ctx, store, tenantID, createMatchRequest{
		Mode:      "online",
		BoardSize: boardSize,
		WinLength: winLength,
		PlayerID:  req.UserID,
	})
	if err != nil {
		return nil, err
	}
	match.PlayerO = opponent.UserID
	match.LastAction = "match.matched"
	match.UpdatedAt = time.Now().UTC()
	updated, _ := json.Marshal(match)
	if err := store.UpdateEntity(ctx, &database.Entity{ID: match.ID, TenantID: tenantID, Data: updated}); err != nil {
		return nil, err
	}
	_ = store.DeleteEntity(ctx, tenantID, tiktoeQueueID(req.UserID, boardSize, winLength))
	_ = store.DeleteEntity(ctx, tenantID, tiktoeQueueID(opponent.UserID, boardSize, winLength))

	_, _ = store.AppendEvent(ctx, database.Event{
		TenantID: tenantID,
		Topic:    tiktoeTopic(match.ID),
		Key:      match.ID,
		Type:     "tiktoe.match_found",
		Payload:  updated,
	})
	return map[string]any{"status": "matched", "match": match}, nil
}

func tiktoeQueueStatus(ctx context.Context, store *database.Store, tenantID, userID string, boardSize, winLength int) (map[string]any, error) {
	userID = normalizePlayerID(userID)
	boardSize, winLength = sanitizeBoard(boardSize, winLength)
	queueID := tiktoeQueueID(userID, boardSize, winLength)
	_, err := store.GetEntity(ctx, tenantID, queueID)
	if err == nil {
		return map[string]any{"status": "queued"}, nil
	}
	if !errors.Is(err, database.ErrNotFound) {
		return nil, err
	}

	matches, err := store.ListEntities(ctx, tenantID, tiktoeMatchKind, 5000)
	if err != nil {
		return nil, err
	}
	for _, entity := range matches {
		var match tiktoeState
		if json.Unmarshal(entity.Data, &match) != nil {
			continue
		}
		if match.Mode == "online" && (match.PlayerX == userID || match.PlayerO == userID) {
			return map[string]any{"status": "matched", "match": match}, nil
		}
	}
	return map[string]any{"status": "idle"}, nil
}

func loadTiktoeMatch(ctx context.Context, store *database.Store, tenantID, matchID string) (*tiktoeState, error) {
	entity, err := store.GetEntity(ctx, tenantID, matchID)
	if err != nil {
		return nil, err
	}
	var match tiktoeState
	if err := json.Unmarshal(entity.Data, &match); err != nil {
		return nil, err
	}
	return &match, nil
}

func applyTiktoeMove(ctx context.Context, store *database.Store, tenantID, matchID string, req moveRequest) (*tiktoeState, error) {
	match, err := loadTiktoeMatch(ctx, store, tenantID, matchID)
	if err != nil {
		return nil, err
	}
	if match.Winner != "" {
		return nil, errors.New("match already finished")
	}
	if req.Row < 0 || req.Row >= match.BoardSize || req.Col < 0 || req.Col >= match.BoardSize {
		return nil, errors.New("invalid move position")
	}
	if match.Board[req.Row][req.Col] != "" {
		return nil, errors.New("cell already occupied")
	}
	if !isTurnUser(match, req.UserID) {
		return nil, errors.New("not your turn")
	}

	symbol := match.Current
	match.Board[req.Row][req.Col] = symbol
	match.MoveCount++
	match.LastAction = "move.played"
	match.UpdatedAt = time.Now().UTC()

	if winner, cells := detectTiktoeWinner(match.Board, match.WinLength); winner != "" {
		match.Winner = winner
		match.LastAction = fmt.Sprintf("match.winner:%s", winner)
		_ = cells
	} else if tiktoeBoardFull(match.Board) {
		match.Winner = "draw"
		match.LastAction = "match.draw"
	} else {
		if match.Current == "X" {
			match.Current = "O"
		} else {
			match.Current = "X"
		}
	}

	if match.Mode == "bot" && match.Winner == "" && match.Current == "O" {
		botMove(match)
	}

	data, _ := json.Marshal(match)
	if err := store.UpdateEntity(ctx, &database.Entity{ID: match.ID, TenantID: tenantID, Data: data}); err != nil {
		return nil, err
	}
	_, _ = store.AppendEvent(ctx, database.Event{
		TenantID: tenantID,
		Topic:    tiktoeTopic(match.ID),
		Key:      match.ID,
		Type:     "tiktoe.move",
		Payload:  data,
	})
	return match, nil
}

func postTiktoeChat(ctx context.Context, store *database.Store, tenantID, matchID string, req chatRequest) (*chatMessage, error) {
	if strings.TrimSpace(req.UserID) == "" {
		return nil, errors.New("user_id is required")
	}
	if strings.TrimSpace(req.Message) == "" && strings.TrimSpace(req.Emoji) == "" {
		return nil, errors.New("message or emoji is required")
	}
	messageType := "message"
	if strings.TrimSpace(req.Emoji) != "" {
		messageType = "emoji"
	}
	msg := &chatMessage{
		ID:        fmt.Sprintf("chat_%d", time.Now().UnixNano()),
		MatchID:   matchID,
		UserID:    req.UserID,
		Type:      messageType,
		Message:   strings.TrimSpace(req.Message),
		Emoji:     strings.TrimSpace(req.Emoji),
		CreatedAt: time.Now().UTC(),
	}
	data, _ := json.Marshal(msg)
	if err := store.InsertEntity(ctx, &database.Entity{ID: tiktoeChatEntityID(matchID, msg.ID), TenantID: tenantID, Kind: tiktoeChatKind, Data: data}); err != nil {
		return nil, err
	}
	_, _ = store.AppendEvent(ctx, database.Event{
		TenantID: tenantID,
		Topic:    tiktoeTopic(matchID) + ".chat",
		Key:      matchID,
		Type:     "tiktoe.chat",
		Payload:  data,
	})
	return msg, nil
}

func listTiktoeChat(ctx context.Context, store *database.Store, tenantID, matchID string, limit int) ([]chatMessage, error) {
	if limit <= 0 || limit > 200 {
		limit = 50
	}
	entities, err := store.ListEntities(ctx, tenantID, tiktoeChatKind, 5000)
	if err != nil {
		return nil, err
	}
	out := make([]chatMessage, 0, limit)
	for _, entity := range entities {
		if !strings.HasPrefix(entity.ID, "chat:"+matchID+":") {
			continue
		}
		var msg chatMessage
		if json.Unmarshal(entity.Data, &msg) == nil {
			out = append(out, msg)
		}
	}
	if len(out) > limit {
		out = out[len(out)-limit:]
	}
	return out, nil
}

func tiktoeTopic(matchID string) string {
	return "tiktoe.match." + strings.TrimSpace(matchID)
}

func tiktoeQueueID(userID string, boardSize, winLength int) string {
	return fmt.Sprintf("queue:%s:%d:%d", normalizePlayerID(userID), boardSize, winLength)
}

func tiktoeDirectMatchID(userA, userB string, boardSize, winLength int) string {
	a := normalizePlayerID(userA)
	b := normalizePlayerID(userB)
	if b < a {
		a, b = b, a
	}
	return fmt.Sprintf("direct:%s:%s:%d:%d", a, b, boardSize, winLength)
}

func normalizePlayerID(v string) string {
	return strings.TrimSpace(strings.ToLower(v))
}

func tiktoeChatEntityID(matchID, msgID string) string {
	return "chat:" + strings.TrimSpace(matchID) + ":" + strings.TrimSpace(msgID)
}

func sanitizeBoard(size, win int) (int, int) {
	if size < 3 {
		size = 3
	}
	if size > 5 {
		size = 5
	}
	if win < 3 {
		win = 3
	}
	if win > size {
		win = size
	}
	return size, win
}

func botOrEmpty(mode string) string {
	if mode == "bot" {
		return "bot"
	}
	return ""
}

func createBoard(size int) [][]string {
	board := make([][]string, size)
	for i := 0; i < size; i++ {
		board[i] = make([]string, size)
	}
	return board
}

func isTurnUser(match *tiktoeState, userID string) bool {
	if match.Current == "X" {
		return match.PlayerX == userID
	}
	if match.Mode == "bot" {
		return userID == "bot"
	}
	return match.PlayerO == userID
}

func detectTiktoeWinner(board [][]string, winLength int) (string, [][2]int) {
	n := len(board)
	dirs := [][2]int{{0, 1}, {1, 0}, {1, 1}, {1, -1}}
	for r := 0; r < n; r++ {
		for c := 0; c < n; c++ {
			symbol := board[r][c]
			if symbol == "" {
				continue
			}
			for _, dir := range dirs {
				dr, dc := dir[0], dir[1]
				cells := make([][2]int, 0, winLength)
				cells = append(cells, [2]int{r, c})
				for k := 1; k < winLength; k++ {
					nr := r + dr*k
					nc := c + dc*k
					if nr < 0 || nr >= n || nc < 0 || nc >= n {
						break
					}
					if board[nr][nc] != symbol {
						break
					}
					cells = append(cells, [2]int{nr, nc})
				}
				if len(cells) == winLength {
					return symbol, cells
				}
			}
		}
	}
	return "", nil
}

func tiktoeBoardFull(board [][]string) bool {
	for r := range board {
		for c := range board[r] {
			if board[r][c] == "" {
				return false
			}
		}
	}
	return true
}

func botMove(match *tiktoeState) {
	for r := 0; r < match.BoardSize; r++ {
		for c := 0; c < match.BoardSize; c++ {
			if match.Board[r][c] == "" {
				match.Board[r][c] = "O"
				match.MoveCount++
				if winner, _ := detectTiktoeWinner(match.Board, match.WinLength); winner != "" {
					match.Winner = winner
					match.LastAction = "match.winner:O"
					return
				}
				if tiktoeBoardFull(match.Board) {
					match.Winner = "draw"
					match.LastAction = "match.draw"
					return
				}
				match.Current = "X"
				match.LastAction = "bot.move"
				return
			}
		}
	}
}

func withNoControllers(parent context.Context) context.Context {
	return parent
}
