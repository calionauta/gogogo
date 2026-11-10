// Capability registry entry for the dashboard unit.
//
// Split from registry.go for the file-sizes gate; All appends it below.
package capabilities

var dashboardUnits = []Capability{
	{
		ID:      "dashboard",
		Kind:    KindFeature,
		Summary: "signed-in summary page; the dashboard.card slot host",
		DependsOn: []string{
			depSounds,
		},
		Dirs:     []string{"features/dashboard"},
		Register: "features/dashboard/handlers",
		Reason: "Host page for dashboard.card: one unit mounts itself " +
			"through the recipe seam. Remove manually (delete the unit, " +
			"its two policy rows, its Modules entry).",
	},
}
