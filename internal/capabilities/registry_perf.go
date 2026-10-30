// SCOPE:plugin — Capability registry entry for the opt-in diagnostics unit.
//
// Split from registry.go for the file-sizes gate; All appends it below.
package capabilities

var perfUnits = []Capability{
	// perf is the opt-in diagnostics unit: statsviz + pprof behind the
	// operator fence. This comment is the installer strip's start marker.
	{
		ID:         "perf",
		Kind:       KindPlugin,
		Summary:    "opt-in operator diagnostics: statsviz dashboard + stdlib pprof",
		RuntimeOff: "PERF_ENABLED=false (default off in production)",
		Scaffold:   ScaffoldOptIn,
		Dirs:       []string{"internal/perf"},
		Files: []string{
			"internal/server/perf.go",
			"internal/server/perf_test.go",
			"internal/ui/perf.go",
		},
		Note: "Fenced like /console: session + admin + an ADMIN_EMAILS " +
			"match. statsviz is net/http only (no second HTTP stack); pprof is " +
			"stdlib. The installer drops the dependency with the unit.",
		Warns: []string{
			"One-way trim: `gogogo add perf` prints the exact wiring lines; " +
				"re-adding is manual.",
		},
	},
}
