// The kiosk recipe's HTTP slice: one page, two POSTs.
package handlers

import (
	"encoding/json"
	"fmt"
	"net/http"
	"net/url"
	"strings"
	"time"

	"github.com/go-chi/chi/v5"

	"github.com/calionauta/gogogo/config"
	"github.com/calionauta/gogogo/features/kiosk"
	"github.com/calionauta/gogogo/features/kiosk/components"
	"github.com/calionauta/gogogo/features/kiosk/service"
	"github.com/calionauta/gogogo/internal/auth"
	dbgen "github.com/calionauta/gogogo/internal/db/sqlcgen"
	"github.com/calionauta/gogogo/internal/observability"
)

// Handler serves the recipe's surface. Its dependencies arrive through
// register.Deps, the same values the root hands its own units, so a recipe is
// not a special case at boot.
type Handler struct {
	auth *auth.Auth
	cfg  *config.Config
	// orders is the write path; engine decides an order the menu could not.
	// Nil engine means the operator did not enable the classifier, which is the
	// default and the keyless path.
	orders *service.Orders
	engine *kiosk.Classifier
}

// NewHandler builds the slice. A missing auth or store is a wiring bug, and the
// recipe would otherwise dereference it on the first request.
func NewHandler(a *auth.Auth, q *dbgen.Queries, cfg *config.Config) *Handler {
	if a == nil || q == nil {
		panic("kiosk: NewHandler needs auth and queries")
	}
	return &Handler{auth: a, cfg: cfg, orders: service.NewOrders(q), engine: newClassifier(cfg)}
}

// Routes returns the recipe's router with ABSOLUTE patterns, the convention the
// composition root flattens through MountFeature. The patterns must match the
// declaration in .recipe.yml: install checks that they do.
func (h *Handler) Routes() chi.Router {
	r := chi.NewRouter()
	r.Get("/kiosk", h.page)
	r.Post("/api/kiosk/preview", h.preview)
	r.Post("/api/kiosk/confirm", h.confirm)
	return r
}

// session resolves the caller, or sends them to the login page with the reason.
//
// The handler checks the session itself. The RBAC policy grants the path, not
// the data: with RBAC_ENABLED=false (a development default) the policy decides
// nothing at all, and a page that reads one caller's rows without asking who
// they are is the bug this exists to prevent.
func (h *Handler) session(w http.ResponseWriter, r *http.Request) (*auth.ValidatedSession, bool) {
	sess, err := h.auth.Session(r)
	if err != nil || sess == nil || sess.User == nil {
		target := "/login?next=" + url.QueryEscape(r.URL.RequestURI()) + "&auth=required"
		http.Redirect(w, r, target, http.StatusSeeOther)
		return nil, false
	}
	return sess, true
}

const (
	// ordersPageLimit bounds the page's list. One constant: the page and the
	// list-after-confirm patch render the same rows.
	ordersPageLimit = 50
	// shortStampLen is the length of the shortened "YYYY-MM-DD HH:MM" stamp.
	shortStampLen = 16
)

// page renders the caller's own orders.
func (h *Handler) page(w http.ResponseWriter, r *http.Request) {
	sess, ok := h.session(w, r)
	if !ok {
		return
	}
	rows, err := h.orders.List(r.Context(), sess.User.ID, ordersPageLimit)
	if err != nil {
		// Not a fake empty list: an unrunnable query would render as "no orders
		// yet", which is indistinguishable from the real thing.
		observability.Error(r.Context(), "kiosk: list orders failed",
			append(observability.WithComponent("kiosk", observability.LayerFeature), "error", err)...)
		http.Error(w, "listing orders failed", http.StatusInternalServerError)
		return
	}
	w.Header().Set("Content-Type", "text/html; charset=utf-8")
	_ = components.Page(components.PageData{
		Email:       sess.User.Email,
		Role:        sess.Role,
		BuildLabel:  h.cfg.BuildLabel,
		BuildCommit: h.cfg.BuildCommit,
		Phrases:     phrasesJSON(),
		MenuLine:    menuLine(),
		Orders:      orderViews(rows),
	}).Render(r.Context(), w)
}

// phrasesJSON is the recognizer's contextual bias, rendered from the menu so the
// browser cannot hold a second vocabulary.
func phrasesJSON() string {
	b, err := json.Marshal(kiosk.Phrases())
	if err != nil {
		return "[]"
	}
	return string(b)
}

// menuLine is the vocabulary the page shows: the closed set, with the medium
// price, so a customer knows what can be said.
func menuLine() string {
	parts := make([]string, 0, len(kiosk.Drinks))
	for _, d := range kiosk.Drinks {
		parts = append(parts, d.Label+" "+formatPrice(d.BaseCents))
	}
	return strings.Join(parts, " · ")
}

// orderViews converts stored rows into what the page and the panel render,
// resolving IDs to labels and the price once and in one place.
func orderViews(rows []dbgen.ExtKioskOrder) []components.Order {
	out := make([]components.Order, 0, len(rows))
	for _, row := range rows {
		out = append(out, components.Order{
			Drink:     kiosk.DrinkLabel(row.Drink),
			Size:      kiosk.SizeLabel(row.Size),
			Milk:      kiosk.MilkLabel(row.Milk),
			Extras:    kiosk.ExtraLabels(splitExtras(row.Extras)),
			Price:     formatPrice(int(row.PriceCents)),
			When:      displayTime(row.ConfirmedAt),
			Corrected: row.Corrected != 0,
		})
	}
	return out
}

// splitExtras reads the stored column. An empty string is no extras, not one
// empty extra.
func splitExtras(stored string) []string {
	if strings.TrimSpace(stored) == "" {
		return nil
	}
	return strings.Split(stored, "+")
}

// formatPrice renders cents as the menu's currency. Integer arithmetic: a price
// is money, and a float would round it.
func formatPrice(cents int) string {
	if cents < 0 {
		cents = 0
	}
	return fmt.Sprintf("$%d.%02d", cents/100, cents%100)
}

// displayTime shortens an RFC3339 stamp to "YYYY-MM-DD HH:MM". A value that does
// not parse is shown as stored: a timestamp the page cannot read is still a fact
// the user should see, not a blank.
func displayTime(stamp string) string {
	if len(stamp) < shortStampLen {
		return stamp
	}
	if _, err := time.Parse(time.RFC3339, stamp); err != nil {
		return stamp
	}
	return stamp[:10] + " " + stamp[11:16]
}
