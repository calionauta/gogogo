// SCOPE:core — Flat-stack dependency guard (framework-free, one SQLite driver)
//
// Command check-flatdeps proves the flat-stack invariant without booting
// anything: the flat-stack packages must not import frameworks outside
// the blessed set, and database/sql opens must use the Turso driver
// outside the transitional allowlist below. It runs `go list -deps`
// (cached, seconds at most) instead of a build+boot cycle, so violations
// surface at commit time instead of after a 2-minute binary run.
//
// Exit non-zero with the offending import paths when the invariant breaks.
package main

import (
	"context"
	"errors"
	"fmt"
	"os"
	"os/exec"
	"slices"
	"strings"
)

// flatPackages are the import paths that must stay framework-free. The
// list is closed: adding a flat-stack package means adding it here, which
// is exactly the review moment the guarantee needs.
var flatPackages = []string{
	"github.com/calionauta/gogogo/internal/auth",
	"github.com/calionauta/gogogo/internal/db",
	"github.com/calionauta/gogogo/internal/db/sqlcgen",
	"github.com/calionauta/gogogo/internal/natswire",
	"github.com/calionauta/gogogo/internal/queue",
	"github.com/calionauta/gogogo/features/todo/chi",
	"github.com/calionauta/gogogo/features/authviews",
}

// bannedPrefixes are module prefixes that must not appear in any flat
// package's dependency closure: the framework the stack replaced, the
// evaluated-and-rejected auth path (see docs/architecture-decision.md),
// and the unused bundled sqlite driver the template policy forbids.
var bannedPrefixes = []string{
	"github.com/pocketbase/",
	"github.com/thecodearcher/limen",
	"modernc.org/sqlite",
}

// legacyDriverFiles are the transitional sqlite3 opens outside the flat
// stack. They disappear with their owners (the framework boot, the credits
// ledger wiring, the desktop token helper); the list shrinks to empty and
// is deleted then. Everything else must open Turso.
var legacyDriverFiles = []string{
	"db/pocketbase.go",
	"features/credits/credits.go",
	"cmd/gui/main.go",
}

func main() {
	if err := run(); err != nil {
		fmt.Fprintln(os.Stderr, "check-flatdeps:", err)
		os.Exit(1)
	}
}

func run() error {
	for _, pkg := range flatPackages {
		deps, err := listDeps(pkg)
		if err != nil {
			return err
		}
		for _, dep := range deps {
			if banned(dep) {
				return fmt.Errorf("package %s depends on banned %s", pkg, dep)
			}
		}
	}
	if err := checkDriverOpens(); err != nil {
		return err
	}
	fmt.Println("check-flatdeps: flat stack is framework-free")
	return nil
}

// banned reports whether an import path falls under a banned prefix.
func banned(importPath string) bool {
	for _, prefix := range bannedPrefixes {
		if strings.HasPrefix(importPath, prefix) {
			return true
		}
	}
	return false
}

func listDeps(pkg string) ([]string, error) {
	out, err := exec.CommandContext(context.Background(), "go", "list", "-deps", pkg).Output()
	if err != nil {
		return nil, fmt.Errorf("go list %s: %w", pkg, err)
	}
	return strings.Split(strings.TrimSpace(string(out)), "\n"), nil
}

// checkDriverOpens fails on legacy-driver opens outside the allowlist (the
// ncruces driver name, matched literally below). Turso opens pass everywhere.
// Test files are exempt: hermetic :memory: databases keep unit tests fast
// and never ship, while the driver itself leaves go.mod with step 7.
func checkDriverOpens() error {
	out, err := exec.CommandContext(context.Background(), "grep", "-rn", `sql\.Open("sqlite3"`, "--include=*.go",
		"internal", "features", "cmd", "router").Output()
	if err != nil {
		// grep exits 1 on no matches: that is the passing state.
		var exit *exec.ExitError
		if errors.As(err, &exit) && exit.ExitCode() == 1 {
			return nil
		}
		return fmt.Errorf("grep driver opens: %w", err)
	}
	for line := range strings.SplitSeq(strings.TrimSpace(string(out)), "\n") {
		file, _, _ := strings.Cut(line, ":")
		if strings.HasSuffix(file, "_test.go") {
			continue
		}
		if !legacy(file) {
			return fmt.Errorf("non-Turso open outside the transitional list: %s opens sqlite3", line)
		}
	}
	return nil
}

// legacy reports whether a repo-relative file is on the transitional list.
func legacy(file string) bool {
	return slices.Contains(legacyDriverFiles, file)
}
