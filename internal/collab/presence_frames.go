// SCOPE:plugin — presence wire frames shared by every collaborative feature.
//
// The count/leave frames are a CLIENT CONTRACT, not feature prose: whiteboard.js
// and notes.js parse the same `PresenceMsg` shape. Two features emitting their
// own copy of "marshal a roster, broadcast it doc-scoped" is how one of them
// silently drifts and the pill breaks on one page only. This is the one place
// that frame is built.
package collab

import (
	"context"
	"encoding/json"

	"github.com/calionauta/gogogo/internal/observability"
	"github.com/calionauta/gogogo/internal/queue"
)

// BroadcastRoster sends the authoritative peer set for docID to the tabs
// watching it, doc-scoped (a count on docA must never reach docB's pill).
//
// A marshal failure is logged and dropped: presence is ephemeral, the next
// join/leave publishes a fresh roster, and failing the caller's request over an
// unrenderable clientID would be worse than a one-frame gap.
func BroadcastRoster(ctx context.Context, hub *queue.SSEHub, component, docID string, peers []string) {
	msg, err := json.Marshal(PresenceMsg{Doc: docID, Type: "count", Peers: peers})
	if err != nil {
		WarnPresence(ctx, component, "count marshal failed", docID, err)
		return
	}
	hub.BroadcastToDoc(msg, docID)
}

// BroadcastPresence relays one presence frame (join, leave, cursor, caret,
// draft, typing) to docID's tabs, excluding the sender so it never hears its
// own echo — the originator already holds its optimistic view.
func BroadcastPresence(ctx context.Context, hub *queue.SSEHub, component, docID, except string, msg PresenceMsg) {
	msg.Doc = docID
	msg.From = except
	payload, err := json.Marshal(msg)
	if err != nil {
		WarnPresence(ctx, component, "frame marshal failed", docID, err)
		return
	}
	hub.BroadcastExceptToDoc(payload, docID, except)
}

// WarnPresence logs a presence failure with the component and doc attached. Both
// collaborative features log the same fields for the same failures, so the
// helper keeps the vocabulary identical (and out of each handler).
func WarnPresence(ctx context.Context, component, what, docID string, err error) {
	arguments := append(observability.WithComponent(component, "feature"),
		"doc", docID, "error", err)
	observability.Warn(ctx, component+": presence "+what, arguments...)
}
