// HTTP dashboard page: the host for the `dashboard.card` slot.
//
// Two halves, on purpose. The page itself is real (a signed-in user's grid
// of installed recipes, with live counters for the headline two) so that
// hosting the slot is not the whole page; slot fragments — content a recipe
// declares — fill the same grid. A build with no recipes installed renders
// the empty state instead of pretending otherwise.
//
// Two scopes, one page. /dashboard counts the signed-in owner's rows;
// /admin/dashboard counts the whole instance. The admin surface is a separate
// PATH because RBAC is path-based: a query parameter cannot be a policy row.
// It uses the declaration's allQuery when one exists and omits a reading when
// it does not — never by summing owner-scoped counts.
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
	"github.com/calionauta/gogogo/internal/rbac"
	"github.com/calionauta/gogogo/internal/recipe"
	"github.com/calionauta/gogogo/internal/ui"
)

// Handler serves GET /dashboard and the admin-only GET /admin/dashboard.
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
		Routes: []recipe.Route{
			{Method: "GET", Pattern: "/dashboard"},
			{Method: "GET", Pattern: "/admin/dashboard", Role: "admin"},
		},
		Slots: []string{string(recipe.SlotDashboardCard)},
	}
}

func (p recipeProvider) Routes() chi.Router { return p.h.Routes() }

// Routes returns the dashboard router.
func (h *Handler) Routes() chi.Router {
	r := chi.NewRouter()
	r.Get("/dashboard", h.index)
	r.Get("/admin/dashboard", h.adminIndex)
	return r
}

func (h *Handler) index(w http.ResponseWriter, r *http.Request) {
	sess, ok := h.session(w, r)
	if !ok {
		return
	}
	h.render(w, r, sess, sess.User.ID, "mine")
}

// adminIndex is the instance-wide view. The fence is in the handler as well as
// the policy: RBAC_ENABLED defaults to false, so a route alone never guards it.
func (h *Handler) adminIndex(w http.ResponseWriter, r *http.Request) {
	sess, ok := h.session(w, r)
	if !ok {
		return
	}
	if !rbac.IsAdmin(sess.Role) {
		http.Error(w, "forbidden: admin only", http.StatusForbidden)
		return
	}
	h.render(w, r, sess, "", "all")
}

// session bounces guests to /login. A signed-in session is the only caller
// either scope serves.
func (h *Handler) session(w http.ResponseWriter, r *http.Request) (*auth.ValidatedSession, bool) {
	sess, err := h.a.Session(r)
	if err != nil || sess == nil || sess.User == nil {
		http.Redirect(w, r, "/login", http.StatusSeeOther)
		return nil, false
	}
	return sess, true
}

func (h *Handler) render(
	w http.ResponseWriter, r *http.Request,
	sess *auth.ValidatedSession, owner, scope string,
) {
	all := scope == "all"
	c := h.loadCounts(r.Context(), owner, all)
	var cards []recipe.Fragment
	if h.slots != nil {
		cards = h.slots.Fragments(recipe.SlotDashboardCard)
	}
	data := components.PageData{
		Email: sess.User.Email,
		Role:  sess.Role,
		Scope: scope,
		// Every recipe that reports data gets a card, whatever the role:
		// the grid is a summary of the instance, not a menu. The button is
		// the role's business (see buildCards).
		Cards:          buildCards(ui.Modules, ui.Reachable(sess.Role), c, all),
		DashboardCards: cards,
	}
	w.Header().Set("Content-Type", "text/html; charset=utf-8")
	w.Header().Set("Cache-Control", "no-store")
	w.WriteHeader(http.StatusOK)
	_ = components.Page(
		data, h.cfg.BuildLabel, h.cfg.BuildCommit,
	).Render(r.Context(), w)
}

// counts carries every reading the handler could read: declared metrics
// (generic COUNT path) plus the two hook counts with no COUNT query
// (chat messages, credits balance).
type counts struct {
	declared             map[string][]components.Metric
	chatTotal, chatHuman int
	balance              int
}

// loadCounts reads every metric for one scope. Declared metrics run
// generically (see declaredMetrics); hook counts degrade to zero when their
// ref is unwired (trimmed recipe), which is honest — the card still exists.
func (h *Handler) loadCounts(ctx context.Context, owner string, all bool) counts {
	c := counts{}
	c.declared = h.declaredMetrics(ctx, owner, all)
	if !all {
		h.loadHookCounts(ctx, owner, &c)
	}
	return c
}

// loadHookCounts fills the owner-scoped hooks (chat messages, credits
// balance). The admin view omits them: there is no honest instance-wide
// equivalent, and a zero would read as "nothing happened".
func (h *Handler) loadHookCounts(ctx context.Context, owner string, c *counts) {
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
}

// declaredMetrics executes every installed recipe's declared readings for one
// scope. One loop over one list: kind decides which SQL fields to use, so the
// dashboard never learns a second "outcome" concept.
//
// A failing query is LOGGED, not swallowed: the metric vanishes from the grid,
// and a silent skip is how a dashboard loses a card and nobody notices (a
// recipe pointing at a table that no longer exists). In the admin scope a
// reading with no allQuery is omitted the same way, for the same reason: a
// number nobody can compute is not a zero.
func (h *Handler) declaredMetrics(ctx context.Context, owner string, all bool) map[string][]components.Metric {
	out := map[string][]components.Metric{}
	if h.registry == nil || h.db == nil {
		return out
	}
	for _, p := range h.registry.Providers() {
		id := p.ID()
		for _, m := range p.Declaration().Metrics {
			item, ok := h.readMetric(ctx, id, m, owner, all)
			if !ok {
				continue
			}
			out[id] = append(out[id], item)
		}
	}
	return out
}

// readMetric executes one metric in the requested scope. It is the single
// dispatch on kind: activity -> one count; outcome -> success, total and the
// counterMetrics that qualify the ratio.
func (h *Handler) readMetric(
	ctx context.Context, recipeID string, m recipe.Metric, owner string, all bool,
) (components.Metric, bool) {
	if m.ResolvedKind() == recipe.MetricActivity {
		c, ok := countInScope(m.AsCount(), all)
		if !ok {
			return components.Metric{}, false
		}
		n, ok := h.count(ctx, recipeID, c, owner, m.Label)
		if !ok {
			return components.Metric{}, false
		}
		signal := m.Signal
		if signal == "" {
			signal = metricSignal(recipeID, m.Label)
		}
		return components.Metric{
			Kind: recipe.MetricActivity, Label: m.Label, Value: n, Signal: signal,
		}, true
	}
	if m.Success == nil || m.Total == nil {
		return components.Metric{}, false
	}
	successCount, ok := countInScope(*m.Success, all)
	if !ok {
		return components.Metric{}, false
	}
	totalCount, ok := countInScope(*m.Total, all)
	if !ok {
		return components.Metric{}, false
	}
	success, ok := h.count(ctx, recipeID, successCount, owner, m.ID)
	if !ok {
		return components.Metric{}, false
	}
	total, ok := h.count(ctx, recipeID, totalCount, owner, m.ID)
	if !ok {
		return components.Metric{}, false
	}
	item := components.Metric{
		Kind: recipe.MetricOutcome, Label: m.Success.Label,
		Success: success, Total: total,
	}
	// The counterMetrics readings travel with the ratio: a peak in corrections
	// cannot be averaged away by a rise in volume if the number sits beside it.
	for _, c := range m.CounterMetrics {
		scoped, ok := countInScope(c, all)
		if !ok {
			continue
		}
		n, ok := h.count(ctx, recipeID, scoped, owner, c.Label)
		if !ok {
			continue
		}
		item.CounterMetrics = append(item.CounterMetrics, components.CounterMetric{
			Label: c.Label, Value: n,
		})
	}
	return item, true
}

// countInScope picks the owner or all query. A missing allQuery means the
// reading cannot answer the admin question, so it is omitted rather than
// answered from a sum of per-owner counts.
func countInScope(c recipe.Count, all bool) (recipe.Count, bool) {
	if !all {
		return c, true
	}
	if c.AllQuery == "" {
		return recipe.Count{}, false
	}
	return recipe.Count{
		Label: c.Label, Query: c.AllQuery, OwnerBinds: recipe.WithoutOwner(),
	}, true
}

// count runs one declared COUNT for one scope: the owner binds when the metric
// asks for it, and a failure is logged and reported rather than rendered as
// zero.
func (h *Handler) count(
	ctx context.Context, recipeID string, m recipe.Count, owner, name string,
) (int, bool) {
	var n int
	args := []any{owner}
	scope := "owner"
	if !m.BindsOwner() {
		args = nil
		scope = "all"
	}
	if err := h.db.QueryRowContext(ctx, m.Query, args...).Scan(&n); err != nil {
		observability.Warn(ctx, "dashboard: metric query failed",
			append(observability.WithComponent("dashboard", "core"),
				"recipe", recipeID, "metric", name, "scope", scope, "error", err)...)
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
//  1. Declared metrics from the recipe's Declaration (generic path — any
//     bundled or user-installed recipe appears with zero dashboard edits).
//  2. Hook counts for recipes with no COUNT query (chat memory, credits ledger).
//
// Every recipe that reports data gets a card, whatever the role: the grid is a
// summary of the instance, not a menu. The LINK is the role's business — a
// destination the role cannot open (reach says so) renders its numbers with no
// button and a note, because a button that 403s is a broken promise (the same
// rule the rail follows through ui.Visible).
//
// Landing/account/config/dashboard are platform, not recipes with data, and
// never appear here.
func buildCards(mods []ui.Module, reach map[string]bool, c counts, all bool) []components.Card {
	cards := make([]components.Card, 0, len(mods))
	for _, m := range mods {
		switch m.ID {
		case "chat":
			if all {
				continue
			}
			cards = append(cards, components.Card{
				Title: m.Title, URL: m.URL, Cta: cardCta(m, reach, "Open"),
				Locked: !reach[m.ID],
				Metrics: []components.Metric{
					{Kind: recipe.MetricActivity, Label: "Messages", Value: c.chatTotal, Signal: "chatTotal"},
					{Kind: recipe.MetricActivity, Label: "Human", Value: c.chatHuman, Signal: "chatHuman"},
				},
			})
		default:
			metrics, ok := c.declared[m.ID]
			if !ok {
				continue
			}
			cards = append(cards, components.Card{
				Title: m.Title, URL: m.URL, Cta: cardCta(m, reach, "Open"),
				Locked: !reach[m.ID], Metrics: metrics,
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
