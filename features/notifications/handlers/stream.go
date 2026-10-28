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
	owner := sess.User.ID
	clientID := r.URL.Query().Get("clientID")
	if clientID == "" {
		clientID = uuid.NewString()
	}

	// Subscribe BEFORE the handshake. The client treats the initial frame as
	// "ready", and an event delivered between that frame and the subscription
	// was lost — the live-delivery test hung on exactly that window under the
	// race detector (release PR #316).
	wake, cancel := h.subscribeInbox(owner, clientID)
	defer cancel()

	sse := sdk.NewSSE(w, r)
	if !h.writeHandshake(r, sse, owner, clientID) {
		return
	}

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

// subscribeInbox registers this tab's wake channel before the handshake. A
// size-1 channel coalesces bursts and keeps the bus callback non-blocking,
// which is the bus's contract.
func (h *Handler) subscribeInbox(owner, clientID string) (<-chan struct{}, func()) {
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
	return wake, cancel
}

// writeHandshake writes the two opening frames: the ack the shared opener
// waits for, then the inbox itself, so a reconnect paints without a reload.
// The bus subscription is already in place before this runs.
func (h *Handler) writeHandshake(
	r *http.Request, sse *sdk.ServerSentEventGenerator, owner, clientID string,
) bool {
	if aerr := dshelpers.AckStreamOpened(sse, clientID); aerr != nil {
		return false
	}
	if perr := h.patchInbox(r.Context(), sse, owner); perr != nil {
		h.logError(r, "notifications: stream handshake failed", perr)
		return false
	}
	return true
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
