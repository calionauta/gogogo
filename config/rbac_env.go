package config

// loadRBACEnv reads the RBAC env. Split out of Load so the config file
// stays under the file-sizes gate (same rule as loadAuthEnv). Behavior:
// RBAC_ENABLED defaults to true in production and false in development:
// a production deploy starts with the policy enforced, and a single-tenant
// dev loop can opt out explicitly. RBAC_POLICY_SOURCE defaults to "embedded"
// and any value but the exact "db" falls back to it — fail-safe, so an
// unknown source never widens access past the shipped policy.
func (cfg *Config) loadRBACEnv(dev bool) {
	cfg.RBACEnabled = envBool("RBAC_ENABLED", !dev)
	cfg.RBACPermissive = envBool("RBAC_PERMISSIVE", false)
	cfg.RBACPolicySource = getEnv("RBAC_POLICY_SOURCE", "embedded")
	if cfg.RBACPolicySource != "db" {
		cfg.RBACPolicySource = "embedded"
	}
}
