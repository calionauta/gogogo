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
	"database/sql"
	"net/http"

	"github.com/go-chi/chi/v5"

	"github.com/calionauta/gogogo/config"
	"github.com/calionauta/gogogo/features/dashboard/components"
	"github.com/calionauta/gogogo/internal/auth"
	dbgen "github.com/calionauta/gogogo/internal/db/sqlcgen"
	"github.com/calionauta/gogogo/internal/observability"
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
	registry   *recipe.Registry
	db         *sql.DB
	chatCount  func(ctx context.Context, owner string) (total, human int, err error)
	creditsBal func(ctx context.Context, owner string) (int64, error)
}

// SetRegistry attaches the boot registry and raw store (nil-safe: no
// declared metrics then, cards fall back to the hook counts only).
func (h *Handler) SetRegistry(reg *recipe.Registry, db *sql.DB) {
	h.registry = reg
	h.db = db
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
	c := h.loadCounts(r.Context(), sess.User.ID)
	var cards []recipe.Fragment
	if h.slots != nil {
		cards = h.slots.Fragments(recipe.SlotDashboardCard)
	}
	data := components.PageData{
		Email: sess.User.Email,
		Role:  sess.Role,
		// Every recipe that reports data gets a card, whatever the role:
		// the grid is a summary of the instance, not a menu. The button is
		// the role's business (see buildCards).
		Cards:          buildCards(ui.Modules, ui.Reachable(sess.Role), c),
		DashboardCards: cards,
	}
	w.Header().Set("Content-Type", "text/html; charset=utf-8")
	w.Header().Set("Cache-Control", "no-store")
	w.WriteHeader(http.StatusOK)
	_ = components.Page(
		data, h.cfg.BuildLabel, h.cfg.BuildCommit,
	).Render(r.Context(), w)
}

// counts carries every metric the handler could read: declared metrics
// (generic COUNT path) plus the two hook counts with no COUNT query
// (chat messages, credits balance).
type counts struct {
	declared             map[string][]components.Metric
	outcomes             map[string][]components.Outcome
	chatTotal, chatHuman int
	balance              int
}

// loadCounts reads every metric for one owner. Declared metrics run
// generically (see declaredMetrics); hook counts degrade to zero when
// their ref is unwired (trimmed recipe), which is honest — the card
// still exists (see buildCards).
func (h *Handler) loadCounts(ctx context.Context, owner string) counts {
	var c counts
	// Declared metrics: every provider's Declaration (bundled or
	// user-installed) is executed here generically — one COUNT query
	// per metric, bound to the viewing owner. No per-recipe switch.
	c.declared = h.declaredMetrics(ctx, owner)
	c.outcomes = h.declaredOutcomes(ctx, owner)
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
	return c
}

// declaredMetrics executes every installed recipe's declared COUNT
// metrics for one owner. Fail-fast on a page error is the caller's
// choice; a single bad query degrades to omitting that metric (the
// registry Validate already proved the shape at boot).
//
// A failing query is LOGGED, not swallowed: the metric vanishes from the
// grid, and a silent skip is how a dashboard loses a card and nobody
// notices (a recipe pointing at a table that no longer exists).
func (h *Handler) declaredMetrics(ctx context.Context, owner string) map[string][]components.Metric {
	out := map[string][]components.Metric{}
	if h.registry == nil || h.db == nil {
		return out
	}
	for _, p := range h.registry.Providers() {
		id := p.ID()
		for _, m := range p.Declaration().Metrics {
			n, ok := h.count(ctx, id, m, owner, "metric", m.Label)
			if !ok {
				continue
			}
			signal := m.Signal
			if signal == "" {
				signal = metricSignal(id, m.Label)
			}
			out[id] = append(out[id], components.Metric{Label: m.Label, Value: n, Signal: signal})
		}
	}
	return out
}

// declaredOutcomes executes every installed recipe's declared outcome pair for
// one owner. Two COUNTs per outcome, the same generic path as a metric (see
// count) — the declaration carries the arithmetic, so no per-recipe switch and
// no second query language appear here. A failing half omits the outcome
// rather than rendering a ratio with an invented number.
func (h *Handler) declaredOutcomes(ctx context.Context, owner string) map[string][]components.Outcome {
	out := map[string][]components.Outcome{}
	if h.registry == nil || h.db == nil {
		return out
	}
	for _, p := range h.registry.Providers() {
		id := p.ID()
		for _, o := range p.Declaration().Outcomes {
			success, ok := h.count(ctx, id, o.Success, owner, "outcome", o.ID)
			if !ok {
				continue
			}
			total, ok := h.count(ctx, id, o.Total, owner, "outcome", o.ID)
			if !ok {
				continue
			}
			out[id] = append(out[id], components.Outcome{
				Label: o.Success.Label, Success: success, Total: total,
			})
		}
	}
	return out
}

// count runs one declared COUNT for one owner: the owner binds when the metric
// asks for it, and a failure is logged and reported rather than rendered as
// zero. Metrics and outcomes share it so "a declared count" has one executor —
// but each keeps its own log line and field name, because the operator looking
// at a missing card is diagnosing a metric or an outcome, not a generic count.
func (h *Handler) count(
	ctx context.Context, recipeID string, m recipe.Metric, owner, kind, name string,
) (int, bool) {
	var n int
	args := []any{owner}
	if !m.BindsOwner() {
		args = nil
	}
	if err := h.db.QueryRowContext(ctx, m.Query, args...).Scan(&n); err != nil {
		msg, field := "dashboard: metric query failed", "metric"
		if kind == "outcome" {
			msg, field = "dashboard: outcome query failed", "outcome"
		}
		observability.Warn(ctx, msg,
			append(observability.WithComponent("dashboard", "core"),
				"recipe", recipeID, field, name, "error", err)...)
		return 0, false
	}
	return n, true
}

// metricSignal derives the odometer signal for a declared metric
// without an explicit Signal: camelCase of the label ("Open Todos" ->
// "openTodos"). Deterministic, so YAML and code agree by convention.
const (
	asciiLower = 'a' - 'A' // 'a'-'A' = 32 (toLowercase offset, named for mnd)
)

func metricSignal(recipeID, label string) string {
	var out []rune
	lowerNext := true
	for _, r := range label {
		if r == ' ' || r == '_' || r == '-' {
			lowerNext = true
			continue
		}
		if lowerNext && r >= 'A' && r <= 'Z' {
			r += asciiLower
		}
		out = append(out, r)
		lowerNext = false
	}
	_ = recipeID
	return string(out)
}

// buildCards lists the metric cards for this build: one card per rail
// destination that reports data. Sources, in order:
//  1. Declared COUNT metrics from the recipe's Declaration (generic
//     path — any bundled or user-installed recipe appears with zero
//     dashboard edits).
//  2. Hook counts for recipes with no COUNT query (chat memory,
//     credits ledger).
//
// Every recipe that reports data gets a card, whatever the role: the grid
// is a summary of the instance, not a menu. The LINK is the role's
// business — a destination the role cannot open (reach says so) renders
// its numbers with no button and a note, because a button that 403s is a
// broken promise (the same rule the rail follows through ui.Visible).
//
// Landing/account/config/dashboard are platform, not recipes with
// data, and never appear here.
func buildCards(mods []ui.Module, reach map[string]bool, c counts) []components.Card {
	cards := make([]components.Card, 0, len(mods))
	for _, m := range mods {
		switch m.ID {
		case "chat":
			cards = append(cards, components.Card{
				Title: m.Title, URL: m.URL, Cta: cardCta(m, reach, "Open Chat"),
				Locked: !reach[m.ID],
				Metrics: []components.Metric{
					{Label: "Messages", Value: c.chatTotal, Signal: "chatTotal"},
					{Label: "Human", Value: c.chatHuman, Signal: "chatHuman"},
				},
			})
		default:
			metrics, ok := c.declared[m.ID]
			if !ok {
				continue
			}
			cards = append(cards, components.Card{
				Title: m.Title, URL: m.URL, Cta: cardCta(m, reach, "Open "+m.Title),
				Locked: !reach[m.ID], Metrics: metrics, Outcomes: c.outcomes[m.ID],
			})
		}
	}
	return cards
}

// cardCta is the card's button: the label for a destination the role may
// open, and nothing at all for one it may not (the card still shows its
// numbers, marked as out of reach).
func cardCta(m ui.Module, reach map[string]bool, label string) string {
	if !reach[m.ID] {
		return ""
	}
	return label
}
