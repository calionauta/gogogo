// graph feature (scaffolded by `gogogo new`)
//
// Two routes: an auth-gated page and a JSON probe. Wiring lives in
// internal/server/chi.go (import, construction, one MountFeature call) and the
// RBAC rows in internal/rbac/casbin/policy.csv. Grow the feature by adding
// methods to Routes and a `p, user, <path>, .*` row for each new path.
package handlers

import (
	"context"
	"encoding/json"
	"net/http"
	"strconv"

	"github.com/go-chi/chi/v5"

	"github.com/calionauta/gogogo/config"
	"github.com/calionauta/gogogo/features/graph/components"
	"github.com/calionauta/gogogo/internal/auth"
	graphservice "github.com/calionauta/gogogo/internal/graph"
)

// Handler serves the graph feature on chi. Workflow defs are injected at
// boot (BootChi passes the DagNats constants), so trimming the engine
// leaves no dangling import here — the endpoint answers with an empty
// document instead.
type Handler struct {
	auth *auth.Auth
	cfg  *config.Config
	defs []string
	runs RunFetcher
}

// SetWorkflowDefs wires the durable workflow JSONs the DagNats source
// draws from. Nil (engine trimmed/disabled) is the empty-workflows case.
func (h *Handler) SetWorkflowDefs(defs []string) { h.defs = defs }

// RunFetcher lists recent runs for live overlay. Nil means defs-only
// (engine trimmed or unreachable); the endpoint degrades instead of 500.
type RunFetcher interface {
	ListRuns(ctx context.Context, workflow string, limit int) ([]graphservice.Run, error)
}

// SetRunFetcher wires the live-run seam. The composition root passes an
// adapter over the DagNats client; the graph unit itself never imports it.
func (h *Handler) SetRunFetcher(f RunFetcher) { h.runs = f }

// NewHandler builds the chi handler. Nil deps panic: wire errors surface at
// boot, not per request.
func NewHandler(a *auth.Auth, cfg *config.Config) *Handler {
	if a == nil {
		panic("graph chi: NewHandler with nil auth")
	}
	if cfg == nil {
		panic("graph chi: NewHandler with nil config")
	}
	return &Handler{auth: a, cfg: cfg}
}

// Routes returns the chi router with the page, the JSON sources and the probe.
func (h *Handler) Routes() chi.Router {
	r := chi.NewRouter()
	r.Get("/graph", h.handlePage)
	r.Get("/api/graph/panorama", h.handlePanorama)
	r.Get("/api/graph/dagnats", h.handleDagNats)
	r.Get("/api/graph/ping", h.handlePing)
	return r
}

// handlePanorama returns the system map as JSON. Auth is checked in the
// handler itself: RBAC_ENABLED defaults to false, so a policy row alone
// never guards a route.
func (h *Handler) handlePanorama(w http.ResponseWriter, r *http.Request) {
	w.Header().Set("Content-Type", "application/json")
	if _, err := h.auth.Session(r); err != nil {
		w.WriteHeader(http.StatusUnauthorized)
		_ = json.NewEncoder(w).Encode(map[string]string{errorKey: authRequired})
		return
	}
	recipes, err := graphservice.LoadInstalledRecipes(".")
	if err != nil {
		w.WriteHeader(http.StatusInternalServerError)
		_ = json.NewEncoder(w).Encode(map[string]string{errorKey: "graph load failed"})
		return
	}
	doc := graphservice.Panorama(recipes)
	_ = json.NewEncoder(w).Encode(doc)
}

// handleDagNats returns the durable workflows the binary owns as a
// node/edge document, overlaid with live run status when a fetcher is
// wired (?live=1, the default). ?workflow= narrows both defs and runs.
// Same auth stance as the panorama: the handler checks the session itself.
// With no defs injected (engine trimmed) the document is empty, never a
// dangling import. A failing run fetch degrades to defs-only — the canvas
// must never blank because the engine is down.
func (h *Handler) handleDagNats(w http.ResponseWriter, r *http.Request) {
	w.Header().Set("Content-Type", "application/json")
	if _, err := h.auth.Session(r); err != nil {
		w.WriteHeader(http.StatusUnauthorized)
		_ = json.NewEncoder(w).Encode(map[string]string{errorKey: authRequired})
		return
	}
	workflow := r.URL.Query().Get("workflow")
	defs := make([]graphservice.WorkflowDef, 0, len(h.defs))
	for _, raw := range h.defs {
		def, err := graphservice.ParseWorkflow([]byte(raw))
		if err != nil {
			w.WriteHeader(http.StatusInternalServerError)
			_ = json.NewEncoder(w).Encode(map[string]string{errorKey: "dagnats parse failed"})
			return
		}
		if workflow != "" && def.Name != workflow {
			continue
		}
		defs = append(defs, def)
	}
	live := r.URL.Query().Get("live") != "0"
	if h.runs != nil && live {
		limit, _ := strconv.Atoi(r.URL.Query().Get("limit"))
		if limit <= 0 {
			limit = 20
		}
		runs, ferr := h.runs.ListRuns(r.Context(), workflow, limit)
		if ferr != nil {
			// Degrade, never blank: the defs are still the truth the
			// canvas can show while the engine is unreachable.
			doc := graphservice.Workflows(defs)
			doc.Source = "dagnats:defs-only"
			_ = json.NewEncoder(w).Encode(doc)
			return
		}
		doc := graphservice.OverlayRuns(defs, runs)
		_ = json.NewEncoder(w).Encode(doc)
		return
	}
	doc := graphservice.Workflows(defs)
	_ = json.NewEncoder(w).Encode(doc)
}

// handlePage renders the page for a signed-in user; guests bounce to /login.
func (h *Handler) handlePage(w http.ResponseWriter, r *http.Request) {
	sess, err := h.auth.Session(r)
	if err != nil {
		http.Redirect(w, r, "/login", http.StatusSeeOther)
		return
	}
	email, role := "", ""
	if sess.User != nil {
		email = sess.User.Email
		role = sess.Role
	}
	w.Header().Set("Content-Type", "text/html; charset=utf-8")
	w.WriteHeader(http.StatusOK)
	_ = components.Page(email, role, h.cfg.BuildLabel, h.cfg.BuildCommit).Render(r.Context(), w)
}

// errorKey is the JSON error field name shared by the graph handlers.
const errorKey = "error"

// authRequired is the single body for unauthenticated graph requests.
const authRequired = "auth required"

// handlePing is the cheapest end-to-end check that the route is mounted.
// It checks the session itself (401 JSON, never a redirect): RBAC_ENABLED
// defaults to false, so a policy row alone never guards a route.
func (h *Handler) handlePing(w http.ResponseWriter, r *http.Request) {
	w.Header().Set("Content-Type", "application/json")
	if _, err := h.auth.Session(r); err != nil {
		w.WriteHeader(http.StatusUnauthorized)
		_ = json.NewEncoder(w).Encode(map[string]string{errorKey: authRequired})
		return
	}
	_ = json.NewEncoder(w).Encode(map[string]string{
		"feature": "graph",
		"status":  "ok",
	})
}
