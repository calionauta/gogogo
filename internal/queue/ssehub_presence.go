// SCOPE:core — SSE Hub audience targeting (who receives a fan-out).
//
// BroadcastToUser (one owner's tabs), BroadcastToAllUsers (every login's
// streams — shared-row mutations, each re-listing its own visible set),
// and the per-tab ViewPref store (filter + scope) that record-event
// re-lists preserve. The registry itself (clients/userOf/docOf/viewOf
// maps) stays in ssehub.go; this file only decides audiences.
package queue

// ViewPref is one tab's listing slice: the status filter, the sharing
// scope, and the active search text. Stored per stream clientID so
// record-event re-lists preserve what the tab was looking at.
type ViewPref struct {
	Filter string
	Scope  string
	// Query is the tab's active search text. Empty means "no search" —
	// the list shows everything visible. Kept with the filter/scope so a
	// filter tab click (which carries no q) does not silently drop it.
	Query string
	// Mode is the tab's selected search layer. Kept here for the same reason
	// as Query: a record-event re-list must rank with the layer the user chose,
	// or the order would silently revert to the default mid-session. Empty
	// means "use the operator's default".
	Mode string
}

// ViewOf returns the stored listing slice for clientID, defaulting to
// ("all", "all") for streams that never fetched (or for unknown ids) —
// a tab with no stated preference sees everything visible, never less.
func (h *SSEHub) ViewOf(clientID string) ViewPref {
	h.mu.RLock()
	defer h.mu.RUnlock()
	if v, ok := h.viewOf[clientID]; ok && v.Filter != "" {
		if v.Scope == "" {
			v.Scope = "all"
		}
		return v
	}
	return ViewPref{Filter: "all", Scope: "all"}
}

// SetView stores the listing slice a tab just asked for. Called on every
// list/fragment/search fetch carrying a clientID (tab clicks round-trip, so
// the stored pref tracks the UI without any extra watcher). An empty query
// CLEARS any stored search: that is how the box is emptied, and a fetch
// with no q is the authoritative "show everything" signal for that tab.
func (h *SSEHub) SetView(clientID, filter, scope, query, mode string) {
	if clientID == "" {
		return
	}
	if filter == "" {
		filter = "all"
	}
	if scope == "" {
		scope = "all"
	}
	h.mu.Lock()
	defer h.mu.Unlock()
	// mode is carried over when the caller does not name one: a filter-tab
	// click or a mutation re-list has no opinion about the layer and must not
	// reset the user's choice to the default.
	if mode == "" {
		mode = h.viewOf[clientID].Mode
	}
	h.viewOf[clientID] = ViewPref{Filter: filter, Scope: scope, Query: query, Mode: mode}
}

// BroadcastToAllUsers sends data to all currently connected clients that
// carry a user identity (non-empty userID), EXCEPT excludeClientID.
// This is the shared-row variant of BroadcastToUser: a mutation to a
// 'public' todo must refresh every login's list, not just the owner's —
// while still excluding whiteboard/doc streams, which register without a
// userID and would choke on a todo record event. Each receiving todo
// stream re-lists its own visible set, so no private row ever leaks.
func (h *SSEHub) BroadcastToAllUsers(data []byte, excludeClientID string) {
	h.mu.RLock()
	defer h.mu.RUnlock()
	for id, ch := range h.clients {
		if id == excludeClientID {
			continue
		}
		if h.userOf[id] == "" {
			continue
		}
		h.deliverOrDrop(id, ch, data, "slow-client-broadcast-all-users")
	}
}

// BroadcastToUser sends data to all currently connected clients that
// belong to userID, EXCEPT excludeClientID. This is the per-user scoped
// variant of Broadcast used for record mutations: a mutation to a
// 'private' todo is delivered only to the owning user's open tabs,
// never to another user's (cross-user leak), and never back to the
// originating tab (which already patched its own DOM via the per-request
// SSE response, so a redundant full-list re-render would clobber its
// local view). Drop policy is identical to Broadcast.
func (h *SSEHub) BroadcastToUser(data []byte, userID, excludeClientID string) {
	h.mu.RLock()
	defer h.mu.RUnlock()
	for id, ch := range h.clients {
		if id == excludeClientID {
			continue
		}
		if h.userOf[id] != userID {
			continue
		}
		h.deliverOrDrop(id, ch, data, "slow-client-broadcast-user")
	}
}
