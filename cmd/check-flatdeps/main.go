// SCOPE:core — Flat-stack dependency guard (framework-free, one SQLite driver,
// no cross-feature domain imports, owned sql.Open sites)
//
// Command check-flatdeps proves the flat-stack invariant without booting
// anything: no embedded framework and no duplicate SQLite driver may enter
// the dependency closure; no feature domain may directly import another
// feature's domain (shared UI leaves excepted); and sql.Open may only run
// in the packages that own a database file. An LLM reaching for a familiar
// database, a cross-feature shortcut, or a new sql.Open in a handler would
// silently reintroduce a framework surface, a second pool, or an unowned
// connection — so the bans are explicit with a clear error, not a policy
// paragraph nobody reads. It runs `go list` (cached, seconds at
// most) instead of a build+boot cycle, so violations surface at commit
// time instead of after a 2-minute binary run.
//
// Exit non-zero with the offending import paths when the invariant breaks.
package main

import (
	"bufio"
	"context"
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
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
	"github.com/calionauta/gogogo/features/todo/handlers",
	"github.com/calionauta/gogogo/features/authviews",
}

// bannedPrefixes are module prefixes that must not appear in any flat
// package's dependency closure: an embedded-framework database (the familiar
// one is listed because it is the one an LLM reaches for first), the
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
	if err := checkBannedClosure(); err != nil {
		return err
	}
	if err := checkCrossFeature(); err != nil {
		return err
	}
	if err := checkSQLOpenOwners(); err != nil {
		return err
	}
	fmt.Println("check-flatdeps: flat stack is framework-free")
	return nil
}

func checkBannedClosure() error {
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

// moduleRoot walks up from the working directory to the module root
// (the dir holding go.mod). checkCrossFeature and checkSQLOpenOwners must
// anchor there: `go run` starts at the developer's shell dir, but `go test`
// starts inside the package dir — a relative ./features/... only works in
// the first case (measured: TestRunGreenOnThisTree failed from the package
// dir).
func moduleRoot() (string, error) {
	dir, err := os.Getwd()
	if err != nil {
		return "", err
	}
	for {
		// #nosec G703 -- local dev tool; dir walks up from the caller's own cwd.
		if _, err := os.Stat(filepath.Join(dir, "go.mod")); err == nil {
			return dir, nil
		}
		parent := filepath.Dir(dir)
		if parent == dir {
			return "", fmt.Errorf("go.mod not found above %s", dir)
		}
		dir = parent
	}
}

// featureModule is the import prefix for product packages.
const featureModule = "github.com/calionauta/gogogo/features/"

// sharedLeaves are the only cross-feature imports allowed: pure UI leaves
// with no domain state (authviews/sounds layouts, genui component catalog).
// Everything else must stay inside its own top-level feature so a trim
// (delete directory + wiring call) never breaks another feature.
var sharedLeaves = []string{
	featureModule + "authviews",
	featureModule + "sounds",
	featureModule + "genui/components",
}

// checkCrossFeature rejects direct imports from one feature domain into a
// different one. It inspects direct (non-test) imports via `go list`, so
// _test.go and generated _templ.go never count. Composition roots
// (internal/server, internal/installer, cmd/...) are not under features/
// and are therefore exempt by construction.
func checkCrossFeature() error {
	root, err := moduleRoot()
	if err != nil {
		return err
	}
	// #nosec G204 -- local dev tool; pattern is a literal, dir is the module root.
	cmd := exec.CommandContext(context.Background(), "go", "list",
		"-f", "{{.ImportPath}} {{join .Imports \" \"}}", "./features/...")
	cmd.Dir = root
	out, err := cmd.Output()
	if err != nil {
		return fmt.Errorf("go list ./features/...: %w", err)
	}
	var offenders []string
	for line := range strings.Lines(strings.TrimSpace(string(out))) {
		line = strings.TrimSpace(line)
		if line == "" {
			continue
		}
		imp, imports, ok := strings.Cut(line, " ")
		if !ok {
			continue
		}
		from := topFeature(imp)
		if from == "" {
			continue
		}
		for f := range strings.FieldsSeq(strings.TrimSpace(imports)) {
			to := topFeature(f)
			if to == "" || to == from {
				continue
			}
			if sharedLeaf(f) {
				continue
			}
			offenders = append(offenders, imp+" imports "+f)
		}
	}
	if len(offenders) > 0 {
		return fmt.Errorf("cross-feature domain imports (%d):\n  %s\nonly %v may be shared across features",
			len(offenders), strings.Join(offenders, "\n  "), sharedLeaves)
	}
	return nil
}

// topFeature returns the feature name for a features/ import path:
// features/todo/handlers -> todo. Empty for non-feature paths.
func topFeature(importPath string) string {
	rest, ok := strings.CutPrefix(importPath, featureModule)
	if !ok {
		return ""
	}
	name, _, _ := strings.Cut(rest, "/")
	return name
}

// sharedLeaf reports whether an import path is (or is inside) an allowed
// shared UI leaf.
func sharedLeaf(importPath string) bool {
	for _, leaf := range sharedLeaves {
		if importPath == leaf || strings.HasPrefix(importPath, leaf+"/") {
			return true
		}
	}
	return false
}

// sqlOpenOwners are the only directories that may call sql.Open: each owns
// a database file (app DB, queue DB, credits ledger). Handlers and other
// features must go through internal/db (dbgen.*) or the owning package —
// a new pool per handler against the same SQLite file is never what you
// want. database/sql as a type/constant (sql.ErrNoRows) stays allowed
// everywhere; only the Open call is owned. Scoped to internal/ + features/
// (the SCOPE-linted product trees, same as check-scope) — dev tools under
// cmd/ and rule patterns under rules/ only mention sql.Open in strings.
//
// Limitation: textual scan, so an aliased import (sq.Open) or a call hidden
// in a string-built line evades it. LLM output uses sql.Open in practice;
// ruleguard ModerncDriverOpen covers the driver-name half.
var sqlOpenOwners = []string{"internal/db/", "internal/queue/", "features/credits/"}

func checkSQLOpenOwners() error {
	root, err := moduleRoot()
	if err != nil {
		return err
	}
	var offenders []string
	for _, rel := range []string{"internal", "features"} {
		dir := filepath.Join(root, rel)
		err := filepath.WalkDir(dir, func(path string, d os.DirEntry, err error) error {
			hit, rel, err := sqlOpenViolation(root, path, d, err)
			if err != nil || !hit {
				return err
			}
			offenders = append(offenders, rel)
			return nil
		})
		if err != nil {
			return err
		}
	}
	if len(offenders) > 0 {
		return fmt.Errorf("sql.Open outside owner packages (%d):\n  %s\nowners: %v (use internal/db or dbgen.*)",
			len(offenders), strings.Join(offenders, "\n  "), sqlOpenOwners)
	}
	return nil
}

// isProductSource reports whether path is a hand-written product file the
// sql.Open ownership rule applies to.
func isProductSource(path string) bool {
	if !strings.HasSuffix(path, ".go") {
		return false
	}
	base := filepath.Base(path)
	return !strings.HasSuffix(base, "_test.go") && !strings.HasSuffix(base, "_templ.go")
}

// sqlOpenViolation decides one walked path: (true, rel, nil) when path is a
// hand-written product file outside the owner dirs that calls sql.Open.
// Directories resolve to their skip/continue verdict with no violation.
func sqlOpenViolation(root, path string, d os.DirEntry, walkErr error) (bool, string, error) {
	if walkErr != nil {
		return false, "", walkErr
	}
	if d.IsDir() {
		if name := d.Name(); name == "vendor" || name == "tmp" || name == "node_modules" || strings.HasPrefix(name, ".") {
			return false, "", filepath.SkipDir
		}
		return false, "", nil
	}
	if !isProductSource(path) {
		return false, "", nil
	}
	rel, relErr := filepath.Rel(root, path)
	if relErr != nil {
		return false, "", relErr
	}
	if ownedBy(filepath.ToSlash(rel)) {
		return false, "", nil
	}
	hit, scanErr := fileHasSQLOpen(path)
	if scanErr != nil {
		return false, "", scanErr
	}
	return hit, rel, nil
}

// ownedBy reports whether a module-relative slash path lives under an
// sql.Open owner directory.
func ownedBy(slashed string) bool {
	for _, owner := range sqlOpenOwners {
		if strings.HasPrefix(slashed, owner) {
			return true
		}
	}
	return false
}

// scanBufInit and scanBufMax bound the line scanner for the ownership walk:
// 64 KiB initial, 1 MiB max line (generated-adjacent long lines must not
// abort the scan).
const (
	scanBufInit = 64 * 1024
	scanBufMax  = 1024 * 1024
)

// fileHasSQLOpen textually reports whether path calls sql.Open outside a
// leading-// comment. See sqlOpenOwners for the documented limitation.
func fileHasSQLOpen(path string) (bool, error) {
	f, err := os.Open(path)
	if err != nil {
		return false, err
	}
	defer f.Close()
	sc := bufio.NewScanner(f)
	sc.Buffer(make([]byte, scanBufInit), scanBufMax)
	for sc.Scan() {
		line := strings.TrimSpace(sc.Text())
		if strings.HasPrefix(line, "//") {
			continue
		}
		if strings.Contains(line, "sql.Open(") {
			return true, nil
		}
	}
	return false, sc.Err()
}
