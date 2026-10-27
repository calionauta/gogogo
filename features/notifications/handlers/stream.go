// SCOPE:feature — notifications: the live inbox stream.
//
// One Datastar SSE stream per open tab. The first frame is the open ack the
// shared opener waits for; the second is the inbox itself, so a reconnect
// paints without a reload. After that the stream sleeps until the bus says the
// owner's inbox changed, then re-renders the same `#inbox` region the read
// actions patch.
package handlers

import (
	"context"
	"net/http"
	"time"

	"github.com/google/uuid"
	sdk "github.com/starfederation/datastar-go/datastar"

	"github.com/calionauta/gogogo/config"
	dshelpers "github.com/calionauta/gogogo/internal/datastar"
	"github.com/calionauta/gogogo/internal/observability"

	"github.com/calionauta/gogogo/features/notifications"
)

// handleStream keeps one tab's inbox current.
//
// The session check is the handler's own, not RBAC's: the policy can be off,
// and a stream with no owner has nothing to show.
func (h *Handler) handleStream(w http.ResponseWriter, r *http.Request) {
	sess, err := h.auth.Session(r)
	if err != nil || sess == nil || sess.User == nil {
		http.Error(w, "auth required", http.StatusUnauthorized)
		return
	}
	sse, clientID, ok := h.openStream(w, r, sess.User.ID)
	if !ok {
		return
	}
	owner := sess.User.ID

	// Size-1 wake channel: a burst of deliveries costs one re-render, and a
	// slow tab can never block the publisher.
	wake := make(chan struct{}, 1)
	cancel := h.bus.Subscribe(func(ev notifications.Event) {
		if !wakeFor(ev, owner, clientID) {
			return
		}
		select {
		case wake <- struct{}{}:
		default:
		}
	})
	defer cancel()

	flusher, canFlush := w.(http.Flusher)
	heartbeat := time.NewTicker(config.DefaultSSEHeartbeatInterval)
	defer heartbeat.Stop()

	for {
		select {
		case <-r.Context().Done():
			return
		case <-heartbeat.C:
			if !canFlush {
				continue
			}
			// A revoked session must not keep a live stream. Returning closes
			// it; Datastar reconnects, gets a 401, and the shell turns that
			// into a login redirect — the same path as every other action.
			if _, serr := h.auth.Session(r); serr != nil {
				return
			}
			if _, werr := w.Write([]byte(": heartbeat\n\n")); werr != nil {
				return
			}
			flusher.Flush()
		case <-wake:
			if perr := h.patchInbox(r.Context(), sse, owner); perr != nil {
				h.logError(r, "notifications: stream patch failed", perr)
				return
			}
		}
	}
}

// openStream writes the two opening frames: the ack the shared opener waits
// for, then the inbox itself, so a reconnect paints without a reload.
func (h *Handler) openStream(
	w http.ResponseWriter, r *http.Request, owner string,
) (*sdk.ServerSentEventGenerator, string, bool) {
	clientID := r.URL.Query().Get("clientID")
	if clientID == "" {
		clientID = uuid.NewString()
	}
	sse := sdk.NewSSE(w, r)
	if aerr := dshelpers.AckStreamOpened(sse, clientID); aerr != nil {
		return nil, "", false
	}
	if perr := h.patchInbox(r.Context(), sse, owner); perr != nil {
		h.logError(r, "notifications: stream handshake failed", perr)
		return nil, "", false
	}
	return sse, clientID, true
}

// patchInbox re-reads the owner's inbox and patches the region. The stream
// and the read actions share patchInboxData, so a live push and a click cannot
// drift.
func (h *Handler) patchInbox(ctx context.Context, sse *sdk.ServerSentEventGenerator, owner string) error {
	data, err := h.pageData(ctx, owner)
	if err != nil {
		return err
	}
	//nolint:contextcheck // datastar-go sink boundary: RenderAndPatch renders through templ; the SDK takes no ctx.
	return h.patchInboxData(sse, data)
}

// wakeFor is the stream's routing rule, extracted so a test can pin it: an
// event wakes the owner's tabs, never another owner's, and never the tab whose
// own request already rendered the change.
func wakeFor(ev notifications.Event, owner, clientID string) bool {
	return ev.Owner == owner && ev.Except != clientID
}

// logError is the request-scoped log sink: HTTP slices go through
// internal/observability, never slog directly.
func (h *Handler) logError(r *http.Request, msg string, err error) {
	observability.Error(r.Context(), msg,
		append(observability.WithComponent("notifications", "feature"), "error", err)...)
}
