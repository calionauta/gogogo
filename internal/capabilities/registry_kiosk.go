// Capability registry entry for the kiosk unit.
//
// Split from registry.go for the file-sizes gate; All appends it below.
package capabilities

var kioskUnits = []Capability{
	{
		ID:        "kiosk",
		Kind:      KindFeature,
		Summary:   "voice-first kiosk order demo (closed menu, on-device voice, one declared outcome)",
		DependsOn: []string{idGenui},
		Dirs:      []string{"features/kiosk"},
		Register:  "features/kiosk/handlers",
		Actions:   []string{"confirm"},
		ActionParams: map[string][]string{
			"confirm": {"text", "previewed"},
		},
		Reason: "Bundled like the other demos: the code lives in features/kiosk " +
			"and the receipt in .recipes/kiosk.yml, so it mounts through the " +
			"same seam and `recipe uninstall kiosk` removes it. Its chat panel " +
			"renders through genui — remove it with genui.",
		Note: "The declared outcome (orders that needed no repair over orders " +
			"confirmed) is the worked example of a declared measurement — " +
			"docs/scope-taxonomy.md.",
	},
}
