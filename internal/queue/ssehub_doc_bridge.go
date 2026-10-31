// SCOPE:core — SSE Hub doc-scoped cross-instance bridge (presence fan-out)
package queue

import (
	"encoding/json"
	"log/slog"

	natsio "github.com/nats-io/nats.go"
)

// DocBridgeSubjectPrefix carries DOC-SCOPED hub fan-out between instances.
// One subject per document ("app.docbridge.<docID>"), so a subscriber only
// ever sees traffic for the docs its own tabs watch.
//
// Why its own subject family instead of the two wires that already exist:
//   - app.ephemeral carries Send/Broadcast only; doc fan-out MUST NOT ride it
//     or every doc event would arrive twice on a bridged instance.
//   - app.sync.<doc> / app.notes.<doc> carry CRDT bytes (Loro updates and
//     snapshots). Presence is not a CRDT delta: joining a board is not a
//     document change, and a roster message pushed into the sync worker would
//     be applied as a malformed update and dropped.
//
// One family for both kinds of doc fan-out, because they have exactly the
// same delivery contract: best effort, doc-scoped, no persistence, healed by
// the next event (a join is followed by a count, a leave by a count, a cursor
// by the next cursor).
const DocBridgeSubjectPrefix = "app.docbridge."

// DocBridgeSubscribe is the per-document wildcard a hub subscribes to.
const DocBridgeSubscribe = DocBridgeSubjectPrefix + ">"

// DocBridgeSubject returns the wire subject for one doc's hub fan-out.
func DocBridgeSubject(docID string) string { return DocBridgeSubjectPrefix + docID }

// docBridgeEnvelope is one doc-scoped hub event on the wire. Document is
// carried so the receiver can re-apply the same doc scoping it would have
// applied locally (never trusting the subject alone), and Except preserves
// the no-echo contract: the originator already holds its own optimistic view.
type docBridgeEnvelope struct {
	Doc    string `json:"doc"`
	Except string `json:"except,omitempty"`
	Data   []byte `json:"data"`
}

type docBridge struct {
	nc  *natsio.Conn
	sub *natsio.Subscription
}

// AttachDocBridge makes BroadcastToDoc / BroadcastExceptToDoc cross
// instances. Producers publish once; every instance (the publisher's own
// included) delivers through its local hub, so there is one delivery path and
// no origin tag is needed.
//
// Only the two doc-scoped methods are bridged. Send/Broadcast keep
// app.ephemeral, and the CRDT workers keep their own app.sync/app.notes
// wires; bridging any of them here would duplicate every signal.
//
// A nil connection (NATS disabled) leaves the hub local, unchanged — which is
// the documented single-instance behaviour for whiteboard/notes presence.
func (h *SSEHub) AttachDocBridge(nc *natsio.Conn) error {
	if nc == nil {
		return nil
	}
	h.mu.Lock()
	already := h.docBridge != nil
	h.mu.Unlock()
	if already {
		return nil
	}
	sub, err := nc.Subscribe(DocBridgeSubscribe, func(m *natsio.Msg) {
		var env docBridgeEnvelope
		if uerr := json.Unmarshal(m.Data, &env); uerr != nil {
			slog.Warn("ssehub: doc bridge envelope decode", "error", uerr)
			return
		}
		// Re-apply the doc scope on arrival: the subject already names the
		// doc, and the payload must agree with it, or a mistargeted publish
		// would land on the wrong board.
		if env.Doc == "" || DocBridgeSubject(env.Doc) != m.Subject {
			return
		}
		h.deliverDocLocal(env.Doc, env.Except, env.Data)
	})
	if err != nil {
		return err
	}
	// Flush the SUB before returning: a publish immediately after attach can
	// otherwise race the server's interest registration and be dropped, which
	// is exactly how the bridged-hub tests flake.
	if err := nc.Flush(); err != nil {
		_ = sub.Unsubscribe()
		return err
	}
	h.mu.Lock()
	h.docBridge = &docBridge{nc: nc, sub: sub}
	h.mu.Unlock()
	return nil
}

// publishDocBridge sends one doc-scoped event on the wire. It reports whether
// the event was handed to NATS; a publish failure falls back to local delivery
// so the tabs on this instance never miss a signal because the bus is down.
func (h *SSEHub) publishDocBridge(docID, except string, data []byte) bool {
	h.mu.RLock()
	b := h.docBridge
	h.mu.RUnlock()
	if b == nil {
		return false
	}
	payload, err := json.Marshal(docBridgeEnvelope{Doc: docID, Except: except, Data: data})
	if err != nil {
		return false
	}
	if pubErr := b.nc.Publish(DocBridgeSubject(docID), payload); pubErr != nil {
		slog.Warn("ssehub: doc bridge publish failed, delivering locally", "doc", docID, "error", pubErr)
		return false
	}
	return true
}

// deliverDocLocal is the receiving half: the same doc-scoped fan-out the
// pre-bridge methods performed, run on every instance.
func (h *SSEHub) deliverDocLocal(docID, except string, data []byte) {
	h.mu.RLock()
	defer h.mu.RUnlock()
	for id, ch := range h.clients {
		if id == except {
			continue
		}
		if h.docOf[id] != docID {
			continue
		}
		h.deliverOrDrop(id, ch, data, "slow-client-broadcast-doc")
	}
}
