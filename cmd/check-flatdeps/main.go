// SCOPE:core — Flat-stack dependency guard (framework-free, one SQLite driver)
//
// Command check-flatdeps proves the flat-stack invariant without booting
// anything: the flat-stack packages must not import frameworks outside
// the blessed set — the framework the stack replaced (PocketBase), the
// evaluated-and-rejected auth path, and the bundled sqlite driver the
// template policy forbids. It runs `go list -deps` (cached, seconds at
// most) instead of a build+boot cycle, so violations surface at commit
// time instead of after a 2-minute binary run.
//
// Exit non-zero with the offending import paths when the invariant breaks.
package main

import (
	"context"
	"fmt"
	"os"
	"os/exec"
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
