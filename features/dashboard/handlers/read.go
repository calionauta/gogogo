// The dashboard's headless read: the same summary the page renders, as JSON.
//
// The page and this handler call ONE loadCounts, so a card that lies on screen
// cannot tell the truth to an agent. The payload is the read side of the slice;
// nothing here mutates, which is why `read` is the verb and not an omission.
package handlers

import (
	"context"
	"encoding/json"
	"net/http"

	"github.com/calionauta/gogogo/features/dashboard/components"
	"github.com/calionauta/gogogo/internal/auth"
	"github.com/calionauta/gogogo/internal/observability"
	"github.com/calionauta/gogogo/internal/rbac"
	"github.com/calionauta/gogogo/internal/ui"
)

// The scope vocabulary. Named so a typo in one place is a compile error rather
// than a query the handler reads as the default.
const (
	scopeMine = "mine"
	scopeAll  = "all"
)

// dashboardAPIPath is the read surface. It is an /api/ route because the page
// routes stay HTML: an agent asks for the summary, a browser asks for the page.
const dashboardAPIPath = "/api/dashboard"

// dashboardPayload is what a `read` answers with. Scope is explicit so a caller
// can tell "my numbers" from "the instance's" without knowing the query it sent.
type dashboardPayload struct {
	Scope string            `json:"scope"`
	Cards []components.Card `json:"cards"`
}

// read answers the headless view. It carries the same admin fence as adminIndex:
// RBAC_ENABLED defaults to false, so the handler is the guard, not the route.
func (h *Handler) read(w http.ResponseWriter, r *http.Request, sess *auth.ValidatedSession) {
	_, all, err := dashboardScope(r)
	if err != nil {
		http.Error(w, err.Error(), http.StatusBadRequest)
		return
	}
	if all && !rbac.IsAdmin(sess.Role) {
		http.Error(w, "forbidden: admin only", http.StatusForbidden)
		return
	}
	owner := sess.User.ID
	if all {
		owner = ""
	}
	c := h.loadCounts(r.Context(), owner, all)
	cards := buildCards(r.Context(), ui.Modules, ui.Reachable(sess.Role), c, all)
	writeDashboardJSON(r.Context(), w, cards, all)
}

// dashboardScope is the one place the scope vocabulary is defined: "mine" is
// the default, "all" is the instance view, and anything else is refused rather
// than silently reading as one of them.
func dashboardScope(r *http.Request) (scope string, all bool, err error) {
	switch r.URL.Query().Get("scope") {
	case "", scopeMine:
		return scopeMine, false, nil
	case scopeAll:
		return scopeAll, true, nil
	default:
		return "", false, badScope{got: r.URL.Query().Get("scope")}
	}
}

type badScope struct{ got string }

func (e badScope) Error() string {
	return `unknown scope "` + e.got + `": use "mine" or "all"`
}

// writeDashboardJSON is shared by the handler and its test, which is how the
// test pins the wire shape without rebuilding it.
func writeDashboardJSON(ctx context.Context, w http.ResponseWriter, cards []components.Card, all bool) {
	scope := scopeMine
	if all {
		scope = scopeAll
	}
	w.Header().Set("Content-Type", "application/json; charset=utf-8")
	// The encoder's error text names internal types; log it and answer with a
	// static message so the wire never carries our reflection.
	if err := json.NewEncoder(w).Encode(dashboardPayload{Scope: scope, Cards: cards}); err != nil {
		observability.Error(ctx, "dashboard: encode read",
			observability.WithComponent("dashboard", "core"), "error", err)
		http.Error(w, "could not encode the dashboard summary", http.StatusInternalServerError)
	}
}
