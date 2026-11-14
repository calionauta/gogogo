// Capability registry entry for the panorama graph unit.
//
// Split from registry.go for the file-sizes gate; All appends it below.
package capabilities

var graphUnits = []Capability{
	{
		ID:      "graph",
		Kind:    KindServant,
		Summary: "panorama graph (capabilities + recipes)",
		DependsOn: []string{
			depSounds,
		},
		Dirs:   []string{"features/graph", "internal/graph"},
		Reason: "Delete dirs + MountFeature line.",
	},
}
