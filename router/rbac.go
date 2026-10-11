// SCOPE:core — Opt-in Casbin RBAC middleware (RBAC_ENABLED)
package router

import (
	"context"
	"net/http"
	"sync"

	"github.com/casbin/casbin/v2"
	"github.com/pocketbase/pocketbase/core"

	"github.com/calionauta/gogogo/config"
	"github.com/calionauta/gogogo/internal/observability"
	"github.com/calionauta/gogogo/internal/rbac"
)

// rbacOnce lazily builds the embedded enforcer on the first request that
// reaches the enabled middleware. The build result (including an error) is
// cached so a failure is logged once rather than re-attempted per request.
// errBodyKey is the JSON key for middleware error bodies. One const so a
// client parsing {"error": ...} cannot desync from a renamed key.
const errBodyKey = "error"

var (
	rbacOnce     sync.Once
	rbacEnforcer *casbin.Enforcer
	rbacInitErr  error
)

// rbacMiddleware returns the global RBAC middleware. When RBAC is disabled
// (the default) it is a no-op pass-through, so it can be bound
// unconditionally without changing the default behaviour of the binary.
//
// The subject is a role derived from the request's auth state (see
// rbacSubject); the object is the request path and the action the HTTP
// method. The policy is the go:embed'd CSV in internal/rbac/casbin — tune it
// for your app before enabling RBAC in production.
func rbacMiddleware(cfg *config.Config) func(*core.RequestEvent) error {
	if !cfg.RBACEnabled {
		return func(e *core.RequestEvent) error { return e.Next() }
	}
	return func(e *core.RequestEvent) error {
		rbacOnce.Do(func() {
			rbacEnforcer, rbacInitErr = rbac.NewEnforcer()
		})
		if rbacInitErr != nil {
			observability.Error(context.Background(), "rbac: enforcer init failed",
				"error", rbacInitErr, "component", "router.rbac", "layer", "core")
			return e.JSON(http.StatusInternalServerError, map[string]string{"error": "rbac unavailable"})
		}
		allowed, err := rbac.EnforceWith(rbacEnforcer, rbacSubject(e), e.Request.URL.Path, e.Request.Method)
		if err != nil {
			observability.Error(context.Background(), "rbac: enforce failed",
				"error", err, "component", "router.rbac", "layer", "core")
			return e.JSON(http.StatusInternalServerError, map[string]string{errBodyKey: "rbac error"})
		}
		if !allowed {
			return e.JSON(http.StatusForbidden, map[string]string{errBodyKey: "forbidden"})
		}
		return e.Next()
	}
}

// rbacSubject maps the request's auth state to a policy subject (role). A
// user record may carry a "role" field (the embedded admin rule keys on
// "admin"); when the field is absent, any authenticated user is "user" and a
// request with no session is "anonymous".
func rbacSubject(e *core.RequestEvent) string {
	if e == nil || e.Auth == nil {
		return "anonymous"
	}
	if role := e.Auth.GetString("role"); role != "" {
		return role
	}
	return "user"
}
