// Capability registry entries for the domain-event, analytics and runtime
// recipe-state seams.
//
// Split from registry.go for the file-sizes gate; All appends it below. All
// three are servants: the binary works without them, and the capability they
// serve (hooks, an analytics provider, the admin runtime switch) stops.
package capabilities

var eventsUnits = []Capability{
	{
		ID:      "events",
		Kind:    KindServant,
		Summary: "in-process domain event bus + outbound sinks (webhook, analytics)",
		Dirs:    []string{"internal/events"},
		Files:   []string{"internal/server/event_sinks.go"},
		Reason: "Delete this unit and the bus, then its sinks and their wiring in " +
			"internal/server/event_sinks.go (the constructor the root calls) plus " +
			"EVENTS_*/ANALYTICS_* in config/events.go. A producer's `d.Events.Emit` " +
			"becomes a nil method and stops at the seam, so no slice has to be edited " +
			"to compile. security_events remains the durable audit trail either way.",
		Note: "Best-effort delivery in this cut: a durable outbox is the next step, " +
			"and this package is the seam that turns that into a sink, not a rewrite " +
			"(docs/hooks-analytics.md).",
	},
	{
		ID:         "analytics",
		Kind:       KindServant,
		Summary:    "analytics provider sink for the event bus (off by default)",
		Dirs:       []string{"internal/analytics"},
		Files:      []string{"internal/server/analytics.go"},
		DependsOn:  []string{"events"},
		RuntimeOff: "ANALYTICS_PROVIDER=none",
		Reason: "Removal is manual: delete the provider and internal/server/analytics.go " +
			"(the beacon route and the sink wiring), then the ANALYTICS_* keys in " +
			"config/events.go. It needs the events servant: without a bus there is " +
			"no event to capture, which is why the dependency points that way.",
		Note: "Off by default and key-gated: a provider with no key falls back to " +
			"none, so a typo never sends events somewhere unexpected.",
	},
	{
		ID:        "recipestate",
		Kind:      KindServant,
		Summary:   "runtime recipe enable/disable: the one reader of recipe_state",
		Dirs:      []string{"internal/recipestate"},
		DependsOn: []string{"recipe"},
		Reason: "Removal is manual: delete the reader, internal/server/recipe_state.go " +
			"(boot switch + admin toggle), the SetRecipeState query and the recipe_state " +
			"table's migration becomes append-only residue (migrations are append-only). " +
			"Without it every installed recipe mounts, which is the pre-switch behavior.",
		Note: "The boot switch and the admin Units table read the same predicate, so a " +
			"disabled recipe cannot be shown as enabled. Evaluated once at boot on " +
			"purpose: a disabled unit must not start its workers or tickers.",
	},
}
