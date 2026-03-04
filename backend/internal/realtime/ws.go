package realtime

import (
	"encoding/json"
	"net/http"
	"strings"

	"github.com/gorilla/websocket"
	"hubgame/backend/internal/database"
)

type Handler struct {
	broker   *database.Broker
	upgrader websocket.Upgrader
}

func NewHandler(b *database.Broker) *Handler {
	return &Handler{
		broker: b,
		upgrader: websocket.Upgrader{
			CheckOrigin: func(*http.Request) bool { return true },
		},
	}
}

func (h *Handler) Stream(w http.ResponseWriter, r *http.Request) {
	topic := strings.TrimSpace(r.URL.Query().Get("topic"))
	if topic == "" {
		http.Error(w, "topic is required", http.StatusBadRequest)
		return
	}
	conn, err := h.upgrader.Upgrade(w, r, nil)
	if err != nil {
		return
	}
	defer conn.Close()

	sub := h.broker.Subscribe(r.Context(), topic, 128)
	for ev := range sub {
		payload, _ := json.Marshal(ev)
		if err := conn.WriteMessage(websocket.TextMessage, payload); err != nil {
			return
		}
	}
}
