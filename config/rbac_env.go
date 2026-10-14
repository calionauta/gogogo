package config

// loadRBACEnv reads the RBAC env. Split out of Load so the config file
// stays under the file-sizes gate (same rule as loadAuthEnv). Behavior:
// RBAC_POLICY_SOURCE defaults to "embedded" and any value but the exact
// "db" falls back to it — fail-safe, so an unknown source never widens
// access past the shipped policy.
func (cfg *Config) loadRBACEnv() {
	cfg.RBACEnabled = envBool("RBAC_ENABLED", false)
	cfg.RBACPermissive = envBool("RBAC_PERMISSIVE", false)
	cfg.RBACPolicySource = getEnv("RBAC_POLICY_SOURCE", "embedded")
	if cfg.RBACPolicySource != "db" {
		cfg.RBACPolicySource = "embedded"
	}
}
