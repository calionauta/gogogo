// Installer engine: the search built-in unit (data only)
//
// Split from trim_features.go for the file-sizes gate, mirroring
// trim_dagnats.go. The unit is small, but its rules are the subtlest in the
// package: the plugin owns a package, a consumer half in the todo feature,
// and the Register wiring — and only SOME of those must disappear for the
// tree to compile.

package installer

// searchBuiltinUnit removes the search plugin.
//
// What goes: internal/search, the consumer half of the seam
// (features/todo/handlers/search_source.go etc.), and the search wiring
// inside the todo Register (which validates the mode and calls SetSearch).
// The handler's search SLICE (search.go), the endpoint and the config knob
// stay, on purpose: they hold stdlib-only interfaces and a bool, so they
// compile untouched and degrade to the plain list — the removal contract
// the capability advertises.
var searchBuiltinUnit = []builtinUnit{
	{
		id: unitSearch,
		// The Register search wiring: validation + layers + SetSearch.
		// Without it a trimmed tree keeps importing internal/search from
		// the Register, which would dangle after the plugin goes.
		extraStrips: []fileStrip{
			{
				path: "features/todo/handlers/register.go",
				rules: []stripRule{
					{
						startMarker:       "// Search wiring: every layer, mode from config. The heal is deferred",
						endIsClosingBrace: true,
						endBrace:          "\t}",
					},
				},
			},
		},
		extraDrops: []fileDrop{
			{
				path: "features/todo/handlers/register.go",
				substrs: []string{
					`	"github.com/calionauta/gogogo/internal/search"`,
					// The heal closure stays callable when search is trimmed:
					// it degrades to a no-op in the trimmed tree (the Register
					// keeps the field, the heal body never runs there).
				},
			},
		},
	},
}
