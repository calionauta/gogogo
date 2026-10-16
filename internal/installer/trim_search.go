// SCOPE:plugin — installer engine: the search trim unit (data only)
//
// Split from trim_features.go for the file-sizes gate, mirroring
// trim_dagnats.go. The unit is small, but its rules are the subtlest in the
// package: the plugin owns a package, a consumer half in the todo feature,
// a composition-root call, and a config knob — and only SOME of those must
// disappear for the tree to compile.

package installer

// searchTrimUnit removes the search plugin.
//
// What goes: internal/search, the consumer half of the seam
// (features/todo/handlers/search_source.go) and its tests, the boot helper
// (internal/server/boot_search.go), and the one composition-root call that
// references the helper. Those are exactly the files importing
// internal/search; leaving any of them behind is a dangling import.
//
// What stays, on purpose: the handler's search SLICE (search.go), the
// endpoint, the config knob and the template's box. They hold stdlib-only
// interfaces and a bool, so they compile untouched and degrade to the plain
// list — the removal contract the capability advertises. Dropping the
// endpoint instead would break trim_chi_sync's route accounting.
var searchTrimUnit = []trimUnit{
	{
		id: unitSearch,
		// The composition-root call is the only remaining reference to the
		// dropped helper. It goes as a BLOCK, not a line drop: the
		// `if serr := ...` header alone would orphan its closing brace.
		extraStrips: []fileStrip{
			{
				path: chiGoFile,
				rules: []stripRule{
					{
						startMarker:       "if serr := bootSearch(cfg, todoH, sqlDB); serr != nil {",
						endIsClosingBrace: true,
						endBrace:          "\t}",
					},
				},
			},
		},
	},
}
