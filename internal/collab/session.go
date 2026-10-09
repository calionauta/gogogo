// SCOPE:layer=infra,removal=plugin — Loro CRDT + DocStore + sync workers + presence
package collab

import "encoding/json"

// SessionEvent is the first frame every raw collab SSE stream sends. It
// carries the connection's auth state so a tab can SAY "session expired"
// instead of degrading silently: peer names fall back to client ids, and
// on some topologies live pushes stop arriving, with nothing on screen
// explaining why. One helper for every stream (notes, whiteboard); the
// client renders it through the shared components.SessionBanner.
//
// Returns nil on the impossible marshal error so a caller can skip the
// frame cheaply (map[string]any of a string/bool never fails in practice).
func SessionEvent(docID string, authed bool) []byte {
	b, err := json.Marshal(map[string]any{"type": "session", "doc": docID, "authed": authed})
	if err != nil {
		return nil
	}
	return b
}
