// SCOPE:plugin — Core infrastructure entries kept beside the registry.
package capabilities

var coreExtraUnits = []Capability{
	{
		ID:      "observability",
		Kind:    KindCore,
		Summary: "core observability (slog + tracing)",
		Dirs:    []string{"internal/observability"},
		Note:    "core infra, removal=core",
	},
	{
		ID:      "rbac",
		Kind:    KindCore,
		Summary: "RBAC with Casbin (embedded model + policy, enforced at the mux)",
		Dirs:    []string{"internal/rbac"},
		Note: "Wired middleware, not a dormant package: BootChi builds one " +
			"enforcer and wraps the whole mux (pass-through when " +
			"RBAC_ENABLED=false, enforced denies with 403 + warn log when " +
			"true). Roles come from users.role (migration 0008, ADMIN_EMAILS " +
			"promotes existing rows at boot). /config is admin-only in the handler " +
			"too (rbac.RequireAdmin), so RBAC_ENABLED=false does not expose it; " +
			"the console adds an ADMIN_EMAILS allowlist. The policy test pins " +
			"every mux route for user-or-admin.",
	},
}
