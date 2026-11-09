// HTTP dashboard page: the host for the `dashboard.card` slot.
//
// Two halves, on purpose. The page itself is real (a signed-in user's grid
// of installed recipes, with live counters for the headline two) so that
// hosting the slot is not the whole page; slot fragments — content a recipe
// declares — fill the same grid. A build with no recipes installed renders
// the empty state instead of pretending otherwise.
package handlers

import (
	"net/http"
	"strconv"

	"github.com/go-chi/chi/v5"

	"github.com/calionauta/gogogo/config"
	"github.com/calionauta/gogogo/features/dashboard/components"
	"github.com/calionauta/gogogo/internal/auth"
	dbgen "github.com/calionauta/gogogo/internal/db/sqlcgen"
	"github.com/calionauta/gogogo/internal/recipe"
	"github.com/calionauta/gogogo/internal/ui"
)

// Handler serves GET /dashboard.
type Handler struct {
	cfg *config.Config
	a   *auth.Auth
	q   *dbgen.Queries
	// slots is the dashboard.card surface. Set at construction from the same
	// registry the recipes register into; nil means no recipe may
	// contribute here, and the grid renders its empty state.
	slots *recipe.SlotRegistry
}

// NewHandler builds the dashboard handler. Nil deps panic: wire errors
// surface at boot, not per request. slots may be nil — a caller that offers
// no slots gets the empty grid, never a nil deref.
func NewHandler(cfg *config.Config, a *auth.Auth, q *dbgen.Queries, slots *recipe.SlotRegistry) *Handler {
	if cfg == nil || a == nil || q == nil {
		panic("dashboard: NewHandler with nil dependency")
	}
	return &Handler{cfg: cfg, a: a, q: q, slots: slots}
}

// RecipeID is the recipe id the dashboard unit declares itself under.
const RecipeID = "dashboard"

// Recipe returns the declaration the composition root mounts through the
// recipe seam. It lives beside the routes it describes so the two cannot
// drift without one edit.
func (h *Handler) Recipe() recipe.Provider { return recipeProvider{h: h} }

type recipeProvider struct{ h *Handler }

func (p recipeProvider) ID() string { return RecipeID }

func (p recipeProvider) Declaration() recipe.Declaration {
	return recipe.Declaration{
		Routes: []recipe.Route{{Method: "GET", Pattern: "/dashboard"}},
		Slots:  []string{string(recipe.SlotDashboardCard)},
	}
}

func (p recipeProvider) Routes() chi.Router { return p.h.Routes() }

// Routes returns the dashboard router.
func (h *Handler) Routes() chi.Router {
	r := chi.NewRouter()
	r.Get("/dashboard", h.index)
	return r
}

func (h *Handler) index(w http.ResponseWriter, r *http.Request) {
	sess, err := h.a.Session(r)
	if err != nil || sess == nil || sess.User == nil {
		http.Redirect(w, r, "/login", http.StatusSeeOther)
		return
	}
	rows, lerr := h.q.ListTodosByOwner(r.Context(), sess.User.ID)
	if lerr != nil {
		http.Error(w, "dashboard unavailable", http.StatusInternalServerError)
		return
	}
	open := 0
	for _, row := range rows {
		if !row.Completed {
			open++
		}
	}
	unread, uerr := h.q.CountUnreadInApp(r.Context(), sess.User.ID)
	if uerr != nil {
		http.Error(w, "dashboard unavailable", http.StatusInternalServerError)
		return
	}
	var cards []recipe.Fragment
	if h.slots != nil {
		cards = h.slots.Fragments(recipe.SlotDashboardCard)
	}
	data := components.PageData{
		Email:          sess.User.Email,
		Role:           sess.Role,
		OpenTodos:      open,
		TotalTodos:     len(rows),
		UnreadInbox:    int(unread),
		Cards:          buildCards(ui.Visible(sess.Role), open, len(rows), int(unread)),
		DashboardCards: cards,
	}
	w.Header().Set("Content-Type", "text/html; charset=utf-8")
	w.Header().Set("Cache-Control", "no-store")
	w.WriteHeader(http.StatusOK)
	_ = components.Page(
		data, h.cfg.BuildLabel, h.cfg.BuildCommit,
	).Render(r.Context(), w)
}

// buildCards lists the recipes installed in this build — one card per rail
// destination the role may see. Platform surfaces (the dashboard itself, the
// graph panorama, the admin config) are not recipes and never become a card;
// Config is already absent via MenuOnly, the other two are excluded here by
// id until the nav moves them out of the rail entirely. The headline two
// (todo, inbox) carry their live counters; the rest are static link cards.
func buildCards(mods []ui.Module, open, total, unread int) []components.Card {
	cards := make([]components.Card, 0, len(mods))
	for _, m := range mods {
		if m.ID == "dashboard" || m.ID == "graph" {
			continue
		}
		card := components.Card{Title: m.Title, URL: m.URL}
		switch m.ID {
		case "todo":
			card.Stat = &components.Stat{
				Value:  open,
				Signal: "openTodos",
				Desc:   "of " + strconv.Itoa(total) + " total",
				Cta:    "Open Todo",
			}
		case ui.ModuleInbox:
			card.Stat = &components.Stat{
				Value:  unread,
				Signal: "inboxUnread",
				Desc:   "unread messages",
				Cta:    "Open Inbox",
			}
		}
		cards = append(cards, card)
	}
	return cards
}
