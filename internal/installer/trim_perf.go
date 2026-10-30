// SCOPE:plugin — installer engine: the opt-in diagnostics trim unit.
//
// perf is the first ScaffoldOptIn unit: offered by the scaffold, dropped
// unless the user names it. The trim removes the package, the mount, the menu
// and the dependency; pprof rides the same unit and is stdlib, so nothing
// replaces it on the drop side.
package installer

const (
	serverOperatorFile = "internal/server/operator.go"
	serverConfigFile   = "config/config.go"
	rbacPolicyFile     = "internal/rbac/casbin/policy.csv"
	rbacCoverageFile   = "internal/rbac/coverage_test.go"
	shellTemplFile     = "internal/ui/shell.templ"
	controlsTemplFile  = "features/authviews/controls.templ"
	controlsTestFile   = "features/authviews/controls_test.go"
	assetTestFile      = "features/todo/components/asset_fingerprint_test.go"
	registryPerfFile   = "internal/capabilities/registry_perf.go"
	menuCallFour       = ", false, false)"
	menuCallThree      = ", false)"
)

var perfTrimUnit = []trimUnit{
	{
		id:         unitPerf,
		goModDrops: []string{"github.com/arl/statsviz"},
		extraDrops: []fileDrop{
			{
				path: chiGoFile,
				substrs: []string{
					"perfH := bootPerf(mux, a, cfg, &shutdown)",
					"closePerf(perfH)",
				},
			},
			{
				path:    serverConfigFile,
				substrs: []string{`envBool("PERF_ENABLED", dev),`},
			},
			{
				path:    rbacPolicyFile,
				substrs: []string{"/debug/statsviz", "/debug/pprof"},
			},
			{
				path:    rbacCoverageFile,
				substrs: []string{"/debug/statsviz", "/debug/pprof"},
			},
		},
		extraStrips: []fileStrip{
			{
				path: serverOperatorFile,
				rules: []stripRule{{
					startMarker:       "// perfPageMiddleware seeds",
					endIsClosingBrace: true,
					endBrace:          "}",
				}},
			},
			{
				path: "config/types.go",
				rules: []stripRule{{
					startMarker: "// PerfEnabled mounts",
					endMarker:   "PerfEnabled bool",
				}},
			},
			{
				path: controlsTemplFile,
				rules: []stripRule{
					{
						startMarker: "// on role alone would be a link that always answers 403. `perf` is the",
						endMarker:   "// is offered only next to `operator`, never on its own.",
					},
					{
						startMarker:       "if operator && perf {",
						endIsClosingBrace: true,
						endBrace:          "\t\t\t\t}",
					},
				},
			},
			{
				path: controlsTestFile,
				rules: []stripRule{{
					startMarker:       "// TestUserMenuCarriesRuntimeOnlyForOperatorWithPerf",
					endIsClosingBrace: true,
					endBrace:          "}",
				}},
			},
			{
				path: "config/config_test.go",
				rules: []stripRule{{
					startMarker:       "// TestPerfDefaultsByEnvironment",
					endIsClosingBrace: true,
					endBrace:          "}",
				}},
			},
			{
				path: "internal/capabilities/capabilities_test.go",
				rules: []stripRule{{
					startMarker:       "// TestPerfIsOptIn pins the choice this refactor exists for",
					endIsClosingBrace: true,
					endBrace:          "}",
				}},
			},
			{
				path: registryPerfFile,
				rules: []stripRule{{
					startMarker:       "// perf is the opt-in diagnostics unit",
					endIsClosingBrace: true,
					endBrace:          "\t},",
				}},
			},
		},
		replaces: []fileReplace{
			{
				path:        serverOperatorFile,
				matchSubstr: "perfPageMiddleware(cfg)(",
				old: "\treturn perfPageMiddleware(cfg)(operatorPageMiddleware(cfg, a, engine)" +
					"(inboxBadgeMiddleware(a, q)(next)))",
				newStr: "\treturn operatorPageMiddleware(cfg, a, engine)(inboxBadgeMiddleware(a, q)(next))",
			},
			{
				path:        shellTemplFile,
				matchSubstr: ", Perf(ctx))",
				old:         ", Perf(ctx))",
				newStr:      ")",
			},
			{
				path:        controlsTemplFile,
				matchSubstr: "operator, perf bool",
				old:         "operator, perf bool",
				newStr:      "operator bool",
			},
			{
				path:        controlsTestFile,
				matchSubstr: `"admin", nil, false, false`,
				old:         menuCallFour,
				newStr:      menuCallThree,
			},
			{
				path:        controlsTestFile,
				matchSubstr: `role, nil, false, false`,
				old:         menuCallFour,
				newStr:      menuCallThree,
			},
			{
				path:        controlsTestFile,
				matchSubstr: `"admin", nil, operator, false`,
				old:         ", operator, false)",
				newStr:      ", operator)",
			},
			{
				path:        assetTestFile,
				matchSubstr: `"user", nil, false, false`,
				old:         menuCallFour,
				newStr:      menuCallThree,
			},
			{
				path:        "internal/capabilities/capabilities_test.go",
				matchSubstr: `"search", "perf"`,
				old:         `, "perf"`,
				newStr:      "",
			},
		},
	},
}
