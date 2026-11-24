// Command check-surface is the "wide interface, nothing behind it" gate.
//
// A package exports a name so another package can call it. When nothing
// outside the package ever does, the export is a promise nobody redeemed:
// the surface reads wide, the package is shallow behind it, and `golangci-lint`
// cannot see it — `unused` deliberately ignores exported names, because in a
// library it cannot prove an outside importer is absent. This gate loads the
// whole module at once and proves it.
//
// Why it exists next to `deadcode`: `go tool deadcode` answers a different
// question (is this function reachable from a `main`?) and only about
// functions. Measured on this tree, `deadcode -test ./cmd/web` reports zero
// types, constants and variables; a generated sqlc row type that no query
// selects, a state constant no switch mentions and a sentinel error nobody
// returns are all invisible to it. This gate reports those, and its baseline
// is the allowlist, so the two are complementary rather than duplicated.
//
// Why not `deslop-please` (the Rust linter whose shallow-module rule this
// mirrors): it reads a Cargo workspace and consumes rust-analyzer's SCIP. It
// would index no file here.
//
// Generation is detected with ast.IsGenerated (the stdlib convention, the
// `// Code generated … DO NOT EDIT.` line), not with a `_templ.go`/`.pb.go`
// suffix list: the list always lags the next generator someone adopts.
//
// Two classes, and only the first is a gate by default:
//
//	dead      exported, and no package — this one, a sibling, or a test —
//	          names it. Remove it or unexport it. This is a finding.
//	internal  exported, but the only names naming it are its own package's.
//	          Reported with -include-internal for review: the honest fix is
//	          usually to unexport, but a package may export deliberately
//	          (a documented seam, a test in another package). Not a gate.
//
// Exit codes: 0 clean, 1 findings outside the allowlist, 2 the gate itself
// could not run (a load error must never read as "clean").
package main

import (
	"flag"
	"fmt"
	"os"
	"path/filepath"
	"slices"

	"golang.org/x/tools/go/packages"
)

// modulePath has NO trailing slash: it is a path PREFIX for HasPrefix and a
// JOIN base for package paths, and a trailing slash broke both (it produced
// `module//catalog`, which matched no declaration).
const modulePath = "github.com/calionauta/gogogo"

// modulePath has NO trailing slash: it is a path PREFIX for HasPrefix and a
// JOIN base for package paths, and a trailing slash broke both (it produced
// `module//catalog`, which matched no declaration).
func main() {
	var (
		dir       = flag.String("dir", ".", "module root to check")
		allowPath = flag.String("allow", ".surface-allow", "allowlist file, relative to -dir")
		update    = flag.Bool("update-allow", false, "rewrite the allowlist from the current tree, then exit")
		internal  = flag.Bool("include-internal", false, "also list exports whose only consumers are inside their own package, or another package's test (review mode, not a gate)")
		depth     = flag.Bool("depth", false, "report export depth per package (exports vs. used from outside) instead of judging: the measurement half, never a gate")
		methods   = flag.Bool("methods", false, "also report exported methods that nothing calls and no interface requires")
		minExport = flag.Int("min-export", 8, "with -depth, skip packages offering fewer exported names")
		quiet     = flag.Bool("quiet", false, "print only findings")
	)
	flag.Parse()

	// One load for both reports when -methods is on: loading the module twice
	// (once per report) doubled a 9s gate for no information.
	findings, pkgs := mustScan(*dir, *methods)

	// -depth is a report, not a verdict: it answers "how deep is this package",
	// which is the same measurement read the other way round. It never exits
	// non-zero, so it cannot become a gate by being wired into one.
	if *depth {
		rows := surfaceRows(findings, *minExport)
		harness := 0
		for _, r := range rows {
			note := ""
			if r.harness > 0 {
				note = fmt.Sprintf("  [%d harness-only]", r.harness)
				harness += r.harness
			}
			fmt.Printf("%3d%%  %3d/%3d  %s%s\n",
				100*r.used/max(r.export, 1), r.used, r.export, display(r.pkg), note)
		}
		fmt.Printf("check-surface: %d package(s) with >= %d exports, worst ratio first "+
			"(a low ratio is a wide surface few callers walk through)\n", len(rows), *minExport)
		if harness > 0 {
			fmt.Printf("check-surface: %d name(s) whose ONLY outside consumer is a cmd/ main "+
				"package — alive because a harness calls them, which is how a whole dead "+
				"file stayed reachable\n", harness)
		}
		return
	}

	findings = judge(findings)
	if *update {
		dead := deadNames(findings)
		if err := writeAllow(filepath.Join(*dir, *allowPath), dead); err != nil {
			fmt.Fprintln(os.Stderr, "check-surface: "+err.Error())
			os.Exit(2)
		}
		fmt.Printf("check-surface: wrote %d accepted export(s) to %s\n", len(dead), *allowPath)
		return
	}

	allow, err := readAllow(filepath.Join(*dir, *allowPath))
	if err != nil {
		fmt.Fprintln(os.Stderr, "check-surface: "+err.Error())
		os.Exit(2)
	}

	// An allowlist entry with no matching symbol is not harmless: it hides
	// that the symbol came back.
	stale := staleAllow(allow, findings)
	dead := deadNames(findings)
	fresh := subtracting(dead, allow)
	for _, s := range stale {
		fmt.Fprintf(os.Stderr, "%s: allowlisted, but no exported symbol with that name has zero consumers "+
			"(removed? re-run -update-allow)\n", s)
	}
	for _, f := range findings {
		if f.class != classDead || slices.Contains(allow, f.name) {
			continue
		}
		fmt.Fprintf(os.Stderr, "%s:%d: %s %s is exported and no package names it\n",
			f.pos.Filename, f.pos.Line, f.kind, display(f.name))
	}
	if *internal {
		for _, f := range findings {
			switch f.class {
			case classInternal:
				fmt.Fprintf(os.Stderr, "%s:%d: %s %s is exported; only %s names it (consider unexporting)\n",
					f.pos.Filename, f.pos.Line, f.kind, display(f.name), display(f.pkg))
			case classTestOnly:
				fmt.Fprintf(os.Stderr, "%s:%d: %s %s is exported; only another package's test names it (test seam?)\n",
					f.pos.Filename, f.pos.Line, f.kind, display(f.name))
			}
		}
	}

	var gaps []methodGap
	if *methods {
		gaps = methodGaps(pkgs)
		for _, g := range gaps {
			fmt.Fprintf(os.Stderr, "%s:%d: %s is an exported method nothing calls and no interface requires\n",
				g.pos.Filename, g.pos.Line, display(g.name))
		}
	}

	if *methods {
		if len(gaps) == 0 {
			fmt.Println("check-surface: no exported method is unreachable")
		} else {
			fmt.Fprintf(os.Stderr, "check-surface: %d unreachable method(s)\n", len(gaps))
		}
		os.Exit(0)
	}

	if len(fresh) > 0 || len(stale) > 0 || len(gaps) > 0 {
		if len(fresh) > 0 || len(stale) > 0 {
			fmt.Fprintf(os.Stderr, "check-surface: %d dead export(s), %d new (not in %s)\n",
				len(dead), len(fresh), *allowPath)
		}
		os.Exit(1)
	}
	if !*quiet {
		fmt.Printf("check-surface: %d exported symbol(s) with no outside consumer, all accepted\n", len(dead))
	}
}

// mustScan loads the module once and returns the findings plus the loaded
// packages, so a command that also wants the method report does not pay for a
// second load. A load failure exits: "could not answer" must never read as
// "clean".
func mustScan(dir string, withDeps bool) ([]finding, []*packages.Package) {
	abs, pkgs, err := load(dir, withDeps)
	if err == nil {
		var findings []finding
		findings, err = scanPackages(abs, pkgs)
		if err == nil {
			return findings, pkgs
		}
	}
	fmt.Fprintln(os.Stderr, "check-surface: "+err.Error())
	os.Exit(2)
	return nil, nil
}
