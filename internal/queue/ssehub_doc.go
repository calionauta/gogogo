// SCOPE:core — SSE Hub doc-scoped fan-out (split for size)
package queue

// SetDoc records which collaborative doc clientID is watching (the
// streamTag ServeStream registered with). Doc-scoped fan-out
// (BroadcastToDoc/BroadcastExceptToDoc) delivers only to clients whose
// recorded doc matches. Overwrites on reconnect, so a clientID that
// moves between docs never keeps the old one.
func (h *SSEHub) SetDoc(clientID, docID string) {
	if clientID == "" || docID == "" {
		return
	}
	h.mu.Lock()
	defer h.mu.Unlock()
	h.docOf[clientID] = docID
}

// BroadcastToDoc sends data to all currently connected clients watching
// docID. This is the collaborative-docs variant of Broadcast: a shapes
// or note-text event for one doc must never reach another doc's tabs
// (cross-doc adoption corrupted both sides' views). Clients with no
// recorded doc are skipped. Drop policy is identical to Broadcast.
//
// With AttachDocBridge the event also crosses instances, so a peer tab
// served by instance B sees a join/leave/count that happened on instance A —
// without it, presence was silently per-instance: the pill said "1 online"
// while a peer sat on the same board through another pod.
func (h *SSEHub) BroadcastToDoc(data []byte, docID string) {
	if h.publishDocBridge(docID, "", data) {
		return
	}
	h.deliverDocLocal(docID, "", data)
}

// BroadcastExceptToDoc sends data to all currently connected clients
// watching docID EXCEPT excludeClientID. Same no-echo contract as
// BroadcastExcept (the originator already holds its own optimistic
// state), scoped to one doc so peers on other docs are undisturbed.
func (h *SSEHub) BroadcastExceptToDoc(data []byte, docID, excludeClientID string) {
	if h.publishDocBridge(docID, excludeClientID, data) {
		return
	}
	h.deliverDocLocal(docID, excludeClientID, data)
}
