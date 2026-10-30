// SCOPE:plugin — Capability registry entries for the license/cloud-config slice.
//
// The desktop distribution gate (LICENSE_MODE=license|account) is core wiring:
// the token verifier and sync.json reader are load-bearing when the gate is
// on, and runtime-off when it is not. The activation page is a feature (a
// standalone screen, no shell), so it stays in the manual-removal class.
package capabilities

var licenseUnits = []Capability{
	{
		ID:      "cloudcfg",
		Kind:    KindCore,
		Summary: "local cloud-connection settings (sync.json, 0600)",
		Dirs:    []string{"internal/cloudcfg"},
		Note: "Core: the desktop reads sync.json at boot when NATS_LEAFNODE_URL " +
			"is unset; it is a credential, not app data.",
	},
	{
		ID:      "license",
		Kind:    KindCore,
		Summary: "offline entitlement tokens (Ed25519 license/account activation)",
		Dirs:    []string{"internal/license"},
		Note: "Core: the distribution gate (LICENSE_MODE) verifies locally and " +
			"never phones home; LICENSE_MODE=none leaves it out of the chain.",
	},
	{
		ID:       "license-activation",
		Kind:     KindFeature,
		Summary:  "standalone /activate page for a license or account token",
		Scaffold: ScaffoldManual,
		Dirs:     []string{"features/license"},
		Reason: "Activation page; the gate is runtime-off with LICENSE_MODE=none. " +
			"Removal is manual (drop the route and the page).",
	},
}
