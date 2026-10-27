// SCOPE:core — Composition-root glue for the rail's inbox count.
//
// Two consumers share this file: the page middleware (seed every shell render
// with the owner's count) and the session stream feed (push changes to the
// browser's one leader stream). Keeping both here means page templates stay
// free of delivery-store imports and the auth package stays free of feature
// imports.
package server

import (
	"context"
	"net/http"
	"strings"

	"github.com/calionauta/gogogo/features/notifications"
	"github.com/calionauta/gogogo/internal/auth"
	dbgen "github.com/calionauta/gogogo/internal/db/sqlcgen"
	"github.com/calionauta/gogogo/internal/observability"
	"github.com/calionauta/gogogo/internal/ui"
)

// inboxShellFeed adapts the notifications bus and store to auth's
// SessionStreamExtras: the session stream carries the count, auth never
// imports the feature, and a tree without the inbox leaves the feed nil.
type inboxShellFeed struct {
	bus *notifications.Bus
	q   *dbgen.Queries
}

// Unread returns the owner's unread in-app count.
func (f inboxShellFeed) Unread(ctx context.Context, owner string) (int, error) {
	n, err := f.q.CountUnreadInApp(ctx, owner)
	return int(n), err
}

// Subscribe wakes the leader stream on any change to the owner's inbox. The
// Event.Except field is deliberately ignored: this is the browser's shared
// stream, not the tab that answered its own request, and the other tabs must
// still hear the new count.
func (f inboxShellFeed) Subscribe(owner string, onChange func()) func() {
	return f.bus.Subscribe(func(ev notifications.Event) {
		if ev.Owner == owner {
			onChange()
		}
	})
}

// inboxBadgeMiddleware seeds ui.WithInboxUnread for page navigations. It skips
// the API and asset subtrees, where no rail exists to render, and a missing or
// expired session means there is nothing to count.
func inboxBadgeMiddleware(a *auth.Auth, q *dbgen.Queries) func(http.Handler) http.Handler {
	return func(next http.Handler) http.Handler {
		return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
			if !pageRequest(r) {
				next.ServeHTTP(w, r)
				return
			}
			sess, err := a.Session(r)
			if err != nil || sess == nil || sess.User == nil {
				next.ServeHTTP(w, r)
				return
			}
			n, cerr := q.CountUnreadInApp(r.Context(), sess.User.ID)
			if cerr != nil {
				// A count is decoration, not the page: a store hiccup renders
				// the rail without a bubble instead of failing the navigation.
				observability.Warn(r.Context(), "inbox badge: count unread",
					append(observability.WithComponent("server", "core"), "error", cerr)...)
				next.ServeHTTP(w, r)
				return
			}
			next.ServeHTTP(w, r.WithContext(ui.WithInboxUnread(r.Context(), int(n))))
		})
	}
}

// pageRequest is the middleware's own scope, not a route allowlist: navigations
// and their HEAD probes render the shell; API calls, streams, static assets,
// the service worker, the health probe, the demo-mode endpoint and the proxied
// engine console do not. The prefixes are spelled without the trailing
// separator so the API-route inventory never mistakes this scope check for a
// route.
func pageRequest(r *http.Request) bool {
	if r.Method != http.MethodGet && r.Method != http.MethodHead {
		return false
	}
	switch r.URL.Path {
	case "/sw.js", "/favicon.ico", "/health":
		return false
	}
	const apiRoot, staticRoot, uiRoot = "/api", "/static", "/_ui"
	const consoleRoot, dagnatsRoot = "/console", "/dagnats"
	return !strings.HasPrefix(r.URL.Path, apiRoot+"/") &&
		!strings.HasPrefix(r.URL.Path, staticRoot+"/") &&
		!strings.HasPrefix(r.URL.Path, uiRoot+"/") &&
		!strings.HasPrefix(r.URL.Path, consoleRoot) &&
		!strings.HasPrefix(r.URL.Path, dagnatsRoot)
}
