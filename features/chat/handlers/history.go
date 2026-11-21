// The chat read path: history as JSON, so an agent can read an answer without
// holding an SSE connection open.
//
// Split from chi.go for the file-size gate. The write path (ask/stream/clear)
// stays there; this is the read the recipe's `read` action binds to.
package handlers

import (
	"encoding/json"
	"net/http"
	"strconv"
	"time"

	"github.com/calionauta/gogogo/internal/observability"
)

// historyMessage is one stored record as JSON. Components are rendered HTML
// directives, so they are not echoed here: a headless reader gets what was
// said (role, text, when), which is the answer the stream already showed.
type historyMessage struct {
	Role    string `json:"role"`
	Text    string `json:"text"`
	Created string `json:"created"`
}

// handleMessages is the chat read path: the caller's own history as JSON, so an
// agent can read an answer without holding an SSE connection open. `limit`
// keeps the newest N in created order; absent means the whole thread.
func (h *Handler) handleMessages(w http.ResponseWriter, r *http.Request) {
	sess := h.session(r)
	if sess == nil {
		writeText(w, http.StatusUnauthorized, "sign in to read chat history")
		return
	}
	limit := 0
	if raw := r.URL.Query().Get("limit"); raw != "" {
		n, err := strconv.Atoi(raw)
		if err != nil || n < 1 {
			writeText(w, http.StatusBadRequest, "limit must be a positive integer")
			return
		}
		limit = n
	}
	msgs, err := h.store.List(r.Context(), sess.User.ID)
	if err != nil {
		obsArgs := append(observability.WithComponent("chat", "feature"), "error", err)
		observability.Error(r.Context(), "chat: read history failed", obsArgs...)
		writeText(w, http.StatusInternalServerError, "history unavailable")
		return
	}
	if limit > 0 && len(msgs) > limit {
		msgs = msgs[len(msgs)-limit:]
	}
	out := make([]historyMessage, 0, len(msgs))
	for _, m := range msgs {
		out = append(out, historyMessage{
			Role:    m.Role,
			Text:    m.Text,
			Created: m.Created.UTC().Format(time.RFC3339),
		})
	}
	w.Header().Set("Content-Type", "application/json")
	w.WriteHeader(http.StatusOK)
	_ = json.NewEncoder(w).Encode(map[string]any{
		"messages": out,
		"count":    len(out),
	})
}
