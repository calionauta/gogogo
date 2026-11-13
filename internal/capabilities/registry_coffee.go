// Capability registry entry for the coffee unit.
//
// Split from registry.go for the file-sizes gate; All appends it below.
package capabilities

var coffeeUnits = []Capability{
	{
		ID:        "coffee",
		Kind:      KindFeature,
		Summary:   "voice-first coffee order demo (closed menu, on-device voice, one declared outcome)",
		DependsOn: []string{idGenui},
		Dirs:      []string{"features/coffee"},
		Register:  "features/coffee/handlers",
		Actions:   []string{"confirm"},
		ActionParams: map[string][]string{
			"confirm": {"text", "previewed"},
		},
		Reason: "Bundled like the other demos: the code lives in features/coffee " +
			"and the receipt in .recipes/coffee.yml, so it mounts through the " +
			"same seam and `recipe uninstall coffee` removes it. Its chat panel " +
			"renders through genui — remove it with genui.",
		Note: "The declared outcome (orders that needed no repair over orders " +
			"confirmed) is the worked example of a declared measurement — " +
			"docs/scope-taxonomy.md.",
	},
}
