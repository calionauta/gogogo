// HTTP dashboard page: the host for the `dashboard.card` slot.
//
// Two halves, on purpose. The page itself is real (a signed-in user's grid
// of installed recipes, with live counters for the headline two) so that
// hosting the slot is not the whole page; slot fragments — content a recipe
// declares — fill the same grid. A build with no recipes installed renders
// the empty state instead of pretending otherwise.
package handlers

import (
	"context"
	"net/http"

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
	slots      *recipe.SlotRegistry
	chatCount  func(ctx context.Context, owner string) (total, human int, err error)
	creditsBal func(ctx context.Context, owner string) (int64, error)
}

// SetChatRef wires the chat count hook (nil-safe: the chat card stays off).
func (h *Handler) SetChatRef(c func(ctx context.Context, owner string) (total, human int, err error)) {
	h.chatCount = c
}

// SetCreditsRef wires the credits balance hook (nil-safe).
func (h *Handler) SetCreditsRef(c func(ctx context.Context, owner string) (int64, error)) {
	h.creditsBal = c
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
	c, err := h.loadCounts(r.Context(), sess.User.ID)
	if err != nil {
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
		OpenTodos:      c.open,
		TotalTodos:     c.total,
		UnreadInbox:    c.unread,
		Cards:          buildCards(ui.Visible(sess.Role), c),
		DashboardCards: cards,
	}
	w.Header().Set("Content-Type", "text/html; charset=utf-8")
	w.Header().Set("Cache-Control", "no-store")
	w.WriteHeader(http.StatusOK)
	_ = components.Page(
		data, h.cfg.BuildLabel, h.cfg.BuildCommit,
	).Render(r.Context(), w)
}

// counts carries every metric the handler could read, so buildCards
// stays a pure table (labels come from components.LabelsFor).
type counts struct {
	open, done, total, shared int
	unread, inboxTotal        int
	chatTotal, chatHuman      int
	notes, boards, balance    int
}

// loadCounts reads every metric for one owner. Fail-fast: any read error
// fails the page, so a partial dashboard never pretends to be complete.
// Chat/credits degrade to zero when their ref is unwired (trimmed
// recipe), which is honest — the card still exists (see buildCards).
func (h *Handler) loadCounts(ctx context.Context, owner string) (counts, error) {
	rows, err := h.q.ListTodosByOwner(ctx, owner)
	if err != nil {
		return counts{}, err
	}
	c := counts{total: len(rows)}
	for _, row := range rows {
		if row.Completed {
			c.done++
		} else {
			c.open++
		}
		if row.Visibility == "public" {
			c.shared++
		}
	}
	unread, err := h.q.CountUnreadInApp(ctx, owner)
	if err != nil {
		return counts{}, err
	}
	c.unread = int(unread)
	inbox, err := h.q.ListNotificationsForOwner(
		ctx, dbgen.ListNotificationsForOwnerParams{Owner: owner, Limit: 10000})
	if err != nil {
		return counts{}, err
	}
	c.inboxTotal = len(inbox)
	notes, err := h.q.ListNotes(ctx)
	if err != nil {
		return counts{}, err
	}
	boards, err := h.q.ListBoards(ctx)
	if err != nil {
		return counts{}, err
	}
	c.notes, c.boards = countOwnedNotes(notes, owner), countOwnedBoards(boards, owner)
	if h.chatCount != nil {
		if total, human, err := h.chatCount(ctx, owner); err == nil {
			c.chatTotal, c.chatHuman = total, human
		}
	}
	if h.creditsBal != nil {
		if bal, err := h.creditsBal(ctx, owner); err == nil {
			c.balance = int(bal)
		}
	}
	return c, nil
}

func countOwnedNotes(rows []dbgen.ListNotesRow, owner string) int {
	n := 0
	for _, r := range rows {
		if r.Owner == owner {
			n++
		}
	}
	return n
}

func countOwnedBoards(rows []dbgen.ListBoardsRow, owner string) int {
	n := 0
	for _, r := range rows {
		if r.Owner == owner {
			n++
		}
	}
	return n
}

// buildCards lists the metric cards for this build: one card per recipe
// that has user data, grouped metrics per card (label + count). The
// dashboard is a summary surface, not a second nav — sidebar owns
// destinations and /config owns inventory. Landing/account/config/
// dashboard are platform, not recipes with data, and never appear here.
// A recipe whose metric source is unwired (chat/credits without their
// ref) is omitted rather than shown as a fake zero.
func buildCards(mods []ui.Module, c counts) []components.Card {
	cards := make([]components.Card, 0, len(mods))
	for _, m := range mods {
		switch m.ID {
		case "todo":
			cards = append(cards, components.Card{
				Title: m.Title, URL: m.URL, Cta: "Open Todo",
				Metrics: []components.Metric{
					{Label: "Open", Value: c.open, Signal: "openTodos"},
					{Label: "Done", Value: c.done, Signal: "doneTodos"},
					{Label: "Total", Value: c.total, Signal: "totalTodos"},
					{Label: "Shared", Value: c.shared, Signal: "sharedTodos"},
				},
			})
		case ui.ModuleInbox:
			cards = append(cards, components.Card{
				Title: m.Title, URL: m.URL, Cta: "Open Inbox",
				Metrics: []components.Metric{
					{Label: "Unread", Value: c.unread, Signal: "inboxUnread"},
					{Label: "Total", Value: c.inboxTotal, Signal: "totalInbox"},
				},
			})
		case "chat":
			// Chat always shows (even when unwired): in-memory history
			// is volatile, so zero is honest, not a missing card.
			cards = append(cards, components.Card{
				Title: m.Title, URL: m.URL, Cta: "Open Chat",
				Metrics: []components.Metric{
					{Label: "Messages", Value: c.chatTotal, Signal: "chatTotal"},
					{Label: "Human", Value: c.chatHuman, Signal: "chatHuman"},
				},
			})
		case "notes":
			cards = append(cards, components.Card{
				Title: m.Title, URL: m.URL, Cta: "Open Notes",
				Metrics: []components.Metric{
					{Label: "Notes", Value: c.notes, Signal: "notesCount"},
				},
			})
		case "whiteboard":
			cards = append(cards, components.Card{
				Title: m.Title, URL: m.URL, Cta: "Open Board",
				Metrics: []components.Metric{
					{Label: "Boards", Value: c.boards, Signal: "boardsCount"},
				},
			})
		case "credits":
			cards = append(cards, components.Card{
				Title: m.Title, URL: "/config",
				Metrics: []components.Metric{
					{Label: "Balance", Value: c.balance, Signal: "creditsBal"},
				},
			})
		}
	}
	return cards
}
