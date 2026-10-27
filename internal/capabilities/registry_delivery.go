// SCOPE:plugin — Capability registry entries for the delivery slice.
//
// Split from registry.go for the file-size gate, the same reason the table
// lives in its own file: the slice can grow without pushing the shared table
// over the limit.
package capabilities

// deliveryUnits are appended to All: notifications (the product) and delivery
// (the transport seam it depends on through an injected Sender).
var deliveryUnits = []Capability{
	{
		ID:      "notifications",
		Kind:    KindFeature,
		Summary: "inbox + durable reminders (DagNats sleep → delivery row)",
		Dirs:    []string{"features/notifications"},
		Reason: "Removal is manual: delete this unit, the sidebar entry in " +
			"internal/ui/modules.go, the reminder definition in " +
			"internal/dagnats/reminder.go, the reminder-seed and notification-deliver " +
			"workers plus the onboarding welcome call and the startDagNats argument in " +
			"cmd/web/dagnats.go. Migration 0015 stays (migrations are append-only; " +
			"drop the table with a new one if the data must go). The transport seam " +
			"is the separate `delivery` plugin.",
	},
	{
		ID:   "delivery",
		Kind: KindPlugin,
		Summary: "delivery transport seam: channels (email today) carried by a " +
			"provider (smtp or an external notify engine)",
		RuntimeOff: "DELIVERY_TRANSPORT=off",
		Dirs:       []string{"internal/delivery"},
		Files:      []string{"internal/server/delivery.go"},
		Note: "Without it the inbox still works: the form drops the external " +
			"channel checkbox, and a delivery that asked for one is recorded " +
			"skipped/provider. The template ships in_app and email only; other " +
			"channel values are data a provider may know, not a gogogo surface. " +
			"in_app is never a sender, which keeps one owner for the inbox.",
	},
}
