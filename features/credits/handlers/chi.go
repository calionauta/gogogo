// SCOPE:plugin — HTTP credits slice (AI credits + BYOK)
//
// Same JSON endpoints as the framework routes: balance, managed request,
// BYOK relay, Stripe checkout + webhook. The engine is shared (the Service
// type is driver-agnostic); only the connection comes from Turso here.
// Auth is 401 JSON (never a login redirect); the webhook stays public and
// signature-verified.
package handlers

import (
	"database/sql"
	"encoding/json"
	"fmt"
	"net/http"
	"path/filepath"

	"github.com/go-chi/chi/v5"

	"github.com/calionauta/gogogo/features/credits"
	"github.com/calionauta/gogogo/internal/auth"
	"github.com/calionauta/gogogo/internal/observability"
)

// jsonErrKey is the response body key for error messages.
const jsonErrKey = "error"

// ManagedRequest mirrors the engine's managed-LLM request shape so this
// package never imports the framework-coupled routes file for it.
type ManagedRequest struct {
	Model           string `json:"model"`
	Prompt          string `json:"prompt"`
	MaxOutputTokens int    `json:"maxOutputTokens"`
}

// CheckoutRequest mirrors the engine's Stripe checkout shape.
type CheckoutRequest struct {
	SKU string `json:"sku"`
}

// Handler serves the credits endpoints on chi around a shared engine.
type Handler struct {
	auth *auth.Auth
	svc  *credits.Service
}

// NewHandler builds the chi credits handler. A nil engine (feature
// disabled) is valid: every route answers 503 with a stable body instead
// of nil-panicking, so disabled Richard still boots and probes readably.
func NewHandler(a *auth.Auth, svc *credits.Service) *Handler {
	if a == nil {
		panic("credits chi: NewHandler with nil auth")
	}
	return &Handler{auth: a, svc: svc}
}

// OpenTursoLedger opens the ledger file for the flat stack. Separate file
// from app.db by design: billing rows have their own backup/restore story
// and never share index contention with app reads.
func OpenTursoLedger(dataDir string) (*sql.DB, error) {
	db, err := sql.Open(ledgerDriver, ledgerDSN(filepath.Join(dataDir, "credits.db")))
	if err != nil {
		return nil, fmt.Errorf("credits: open ledger: %w", err)
	}
	return db, nil
}

// Routes returns the chi router with the credits endpoints.
func (h *Handler) Routes() chi.Router {
	r := chi.NewRouter()
	r.Get("/api/credits", h.handleBalance)
	r.Post("/api/ai/request", h.handleManaged)
	r.Post("/api/credits/checkout", h.handleCheckout)
	r.Post("/api/credits/stripe-webhook", h.handleWebhook)
	r.Get("/api/byok/*", h.handleRelay)
	r.Post("/api/byok/*", h.handleRelay)
	r.Put("/api/byok/*", h.handleRelay)
	r.Patch("/api/byok/*", h.handleRelay)
	r.Delete("/api/byok/*", h.handleRelay)
	return r
}

func writeJSON(w http.ResponseWriter, code int, v any) {
	w.Header().Set("Content-Type", "application/json")
	w.WriteHeader(code)
	_ = json.NewEncoder(w).Encode(v)
}

// owner resolves the caller's user id or answers 401 JSON.
func (h *Handler) owner(w http.ResponseWriter, r *http.Request) (string, bool) {
	sess, err := h.auth.Session(r)
	if err != nil || sess.User == nil {
		writeJSON(w, http.StatusUnauthorized, map[string]any{jsonErrKey: "unauthorized"})
		return "", false
	}
	return sess.User.ID, true
}

// engine answers 503 when the feature is disabled.
func (h *Handler) engine(w http.ResponseWriter) (*credits.Service, bool) {
	if h.svc == nil {
		writeJSON(w, http.StatusServiceUnavailable, map[string]any{jsonErrKey: "credits disabled"})
		return nil, false
	}
	return h.svc, true
}

func (h *Handler) handleBalance(w http.ResponseWriter, r *http.Request) {
	uid, ok := h.owner(w, r)
	if !ok {
		return
	}
	svc, ok := h.engine(w)
	if !ok {
		return
	}
	bal, err := svc.Credits.Balance(r.Context(), uid)
	if err != nil {
		// Internal text stays in the log: a 500 body carrying err.Error()
		// can leak driver/schema detail to the client. Same shape as the
		// managed-request path below (log via observability, return a
		// static public message).
		obsArgs := append(observability.WithComponent("credits", "feature"), "error", err)
		observability.Error(r.Context(), "credits: balance lookup failed", obsArgs...)
		writeJSON(w, http.StatusInternalServerError, map[string]any{jsonErrKey: "balance unavailable"})
		return
	}
	writeJSON(w, http.StatusOK, map[string]any{
		"balance": bal,
		"period":  svc.Credits.Period(),
		"mode":    svc.Cfg.DefaultMode,
	})
}

func (h *Handler) handleManaged(w http.ResponseWriter, r *http.Request) {
	uid, ok := h.owner(w, r)
	if !ok {
		return
	}
	svc, ok := h.engine(w)
	if !ok {
		return
	}
	var req ManagedRequest
	dec := json.NewDecoder(r.Body)
	dec.DisallowUnknownFields()
	if err := dec.Decode(&req); err != nil {
		writeJSON(w, http.StatusBadRequest, map[string]any{jsonErrKey: "invalid body"})
		return
	}
	res, err := svc.RunManaged(r.Context(), uid, svc.AppKey, svc.BaseURL, credits.ManagedRequest{
		Model: req.Model, Prompt: req.Prompt, MaxOutputTokens: req.MaxOutputTokens,
	})
	if err != nil {
		obsArgs := append(observability.WithComponent("credits", "feature"), "error", err)
		observability.Warn(r.Context(), "credits: managed request failed", obsArgs...)
		writeJSON(w, http.StatusBadRequest, map[string]any{jsonErrKey: "request failed"})
		return
	}
	writeJSON(w, http.StatusOK, res)
}

func (h *Handler) handleCheckout(w http.ResponseWriter, r *http.Request) {
	uid, ok := h.owner(w, r)
	if !ok {
		return
	}
	svc, ok := h.engine(w)
	if !ok {
		return
	}
	var req CheckoutRequest
	dec := json.NewDecoder(r.Body)
	dec.DisallowUnknownFields()
	if err := dec.Decode(&req); err != nil {
		writeJSON(w, http.StatusBadRequest, map[string]any{jsonErrKey: "invalid body"})
		return
	}
	url, err := svc.HandleCheckout(r.Context(), uid, credits.CheckoutRequest{SKU: req.SKU})
	if err != nil {
		obsArgs := append(observability.WithComponent("credits", "feature"), "error", err)
		observability.Warn(r.Context(), "credits: checkout failed", obsArgs...)
		writeJSON(w, http.StatusBadRequest, map[string]any{jsonErrKey: "checkout failed"})
		return
	}
	writeJSON(w, http.StatusOK, map[string]any{"url": url})
}

func (h *Handler) handleWebhook(w http.ResponseWriter, r *http.Request) {
	svc, ok := h.engine(w)
	if !ok {
		return
	}
	svc.HandleStripeWebhook(w, r)
}

func (h *Handler) handleRelay(w http.ResponseWriter, r *http.Request) {
	uid, ok := h.owner(w, r)
	if !ok {
		return
	}
	svc, ok := h.engine(w)
	if !ok {
		return
	}
	if svc.Relay == nil {
		writeJSON(w, http.StatusNotFound, map[string]any{jsonErrKey: "byok relay not configured"})
		return
	}
	r.Header.Set("X-Auth-User", uid)
	svc.Relay.ServeHTTP(w, r)
}
