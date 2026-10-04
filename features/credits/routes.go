// SCOPE:layer=feature,removal=plugin — HTTP routes for AI credits + BYOK
// (lib ai-credits). To remove: delete features/credits/ + router/credits.go.
//
// Mounted by router.Init only when CREDITS_ENABLED=true. These are JSON
// endpoints; they require an authenticated PocketBase user (401, not a
// /login redirect). The BYOK relay is mounted at /api/byok/ with the authed
// user id stamped into the internal X-Auth-User header the relay reads (the
// relay itself is NOT an auth boundary — see credits/byokrelay.go).
package credits

import (
	"net/http"

	"github.com/pocketbase/pocketbase/core"
)

const (
	// jsonErrKey is the response body key for error messages; kept constant
	// for the golangci goconst linter (used 6+ times across handlers).
	jsonErrKey = "error"

	// unauthorizedMsg is the shared 401 body; kept constant for goconst.
	unauthorizedMsg = "unauthorized"
)

// RegisterRoutes wires the credits endpoints onto the server router.
func (s *Service) RegisterRoutes(se *core.ServeEvent) {
	if s == nil {
		return
	}
	r := se.Router

	r.GET("/api/credits", func(c *core.RequestEvent) error {
		uid, ok := authedUserID(c)
		if !ok {
			return c.JSON(http.StatusUnauthorized, map[string]any{jsonErrKey: unauthorizedMsg})
		}
		bal, err := s.Credits.Balance(c.Request.Context(), uid)
		if err != nil {
			return c.JSON(http.StatusInternalServerError, map[string]any{jsonErrKey: err.Error()})
		}
		return c.JSON(http.StatusOK, map[string]any{
			"balance": bal,
			"period":  s.Credits.Period(),
			"mode":    s.Cfg.DefaultMode,
		})
	})

	// Managed LLM request (billing_mode=managed): reserve, call, settle.
	r.POST("/api/ai/request", func(c *core.RequestEvent) error {
		uid, ok := authedUserID(c)
		if !ok {
			return c.JSON(http.StatusUnauthorized, map[string]any{jsonErrKey: unauthorizedMsg})
		}
		var req ManagedRequest
		if err := c.BindBody(&req); err != nil {
			return c.JSON(http.StatusBadRequest, map[string]any{jsonErrKey: "invalid body"})
		}
		res, err := s.RunManaged(c.Request.Context(), uid, s.AppKey, s.BaseURL, req)
		if err != nil {
			return c.JSON(http.StatusBadRequest, map[string]any{jsonErrKey: err.Error()})
		}
		return c.JSON(http.StatusOK, res)
	})

	// BYOK relay: pass-through to the user's provider with their key.
	//
	// Registered per method, NOT with r.Any("/api/byok/*"). Any() registers an
	// empty method, which becomes a method-less pattern in Go 1.22+ ServeMux and
	// conflicts with the app's own `GET /`:
	//
	//   pattern "/api/byok/*" conflicts with pattern "GET /":
	//   /api/byok/* matches more methods than GET /, but has a more specific
	//   path pattern
	//
	// That panics at route-registration time, so the process dies on startup.
	// It was latent only because this block is skipped when Relay is nil, and
	// Relay is nil unless BYOK is configured — i.e. it would have taken
	// production down the first time someone enabled BYOK. Enumerating methods
	// keeps every pattern method-scoped, so there is nothing to conflict.
	if s.Relay != nil {
		relay := func(c *core.RequestEvent) error {
			uid, ok := authedUserID(c)
			if !ok {
				return c.JSON(http.StatusUnauthorized, map[string]any{jsonErrKey: unauthorizedMsg})
			}
			c.Request.Header.Set("X-Auth-User", uid)
			s.Relay.ServeHTTP(c.Response, c.Request)
			return nil
		}
		for _, method := range []string{
			http.MethodGet,
			http.MethodHead,
			http.MethodPost,
			http.MethodPut,
			http.MethodPatch,
			http.MethodDelete,
			http.MethodOptions,
		} {
			r.Route(method, "/api/byok/*", relay)
		}
	}

	// Stripe top-up: checkout (authed) + webhook (signed, public). Both
	// no-op gracefully when Stripe isn't configured (see stripe.go).
	r.POST("/api/credits/checkout", func(c *core.RequestEvent) error {
		uid, ok := authedUserID(c)
		if !ok {
			return c.JSON(http.StatusUnauthorized, map[string]any{jsonErrKey: unauthorizedMsg})
		}
		var req CheckoutRequest
		if err := c.BindBody(&req); err != nil {
			return c.JSON(http.StatusBadRequest, map[string]any{jsonErrKey: "invalid body"})
		}
		url, err := s.HandleCheckout(c.Request.Context(), uid, req)
		if err != nil {
			return c.JSON(http.StatusBadRequest, map[string]any{jsonErrKey: err.Error()})
		}
		return c.JSON(http.StatusOK, map[string]any{"url": url})
	})
	r.POST("/api/credits/stripe-webhook", func(c *core.RequestEvent) error {
		s.HandleStripeWebhook(c.Response, c.Request)
		return nil
	})
}

// authedUserID returns the authenticated PocketBase user id, or "", false.
func authedUserID(c *core.RequestEvent) (string, bool) {
	if c.Auth == nil {
		return "", false
	}
	id := c.Auth.Id
	if id == "" {
		return "", false
	}
	return id, true
}
