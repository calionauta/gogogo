// The todo page.
//
// Sign-in surfaces (login, signup, logout, recovery) are NOT here: they are
// core, served by internal/authpages. Keeping them in this slice made the
// reference demo the owner of /login, so a product that dropped the slice could
// not sign anyone in.
package handlers

import (
	"net/http"

	"github.com/calionauta/gogogo/features/todo/components"
	"github.com/calionauta/gogogo/internal/demo"
)

// handleIndex serves the demo Todo page on the Layout/TodoPage
// components, with guests bounced to /login and data from dbgen.
func (h *Handler) handleIndex(w http.ResponseWriter, r *http.Request) {
	owner, sess, err := h.ownerSession(r)
	if err != nil {
		redirectLogin(w, r)
		return
	}
	todos, err := h.listVisible(r.Context(), owner, sess.Role, "all", "all")
	if err != nil {
		// Not a fake empty list: an unrunnable query would render as "you
		// have no todos", which is indistinguishable from the real thing and
		// tells the operator nothing. Fail loudly instead — the log carries
		// the cause.
		h.fail(r.Context(), w, "todo: index list failed", err, "error listing todos")
		return
	}
	// The page load is the one render where the URL's ?q= is authoritative: it
	// seeds the box before the client owns $searchQuery (see displaySignals).
	signals := h.displaySignalsWithQuery(r, todos, "all", "all", searchQueryOf(r))
	signals.ConnectedClients = h.Hub.CountUserClients()
	signals.Suggestions = []string{}
	signals.SuggestErr = ""
	signals.RealtimeKind = h.realtimeKind()
	signals.StoreLabel = "Turso Database"
	signals.OfflineLabel = h.offlineLabel()
	signals.SidebarTab = "queue"
	w.Header().Set("Content-Type", "text/html; charset=utf-8")
	w.WriteHeader(http.StatusOK)
	//nolint:contextcheck // templ threads GeneratedComponentInput.Context into
	// nested components; contextcheck cannot see that path, and the ctx here IS
	// the request's (r.Context()).
	_ = components.Layout(
		"Todos — gogogo",
		signals, sess.User.Email, sess.Role,
		h.BuildLabel, h.BuildCommit,
		h.OfflineSyncEnabled,
		demo.On(r),
	).Render(r.Context(), w)
}
