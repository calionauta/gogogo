// SCOPE:core - DO NOT REMOVE - SCOPE annotation linter (pre-commit gate).
// Package main implements the check-scope linter referenced by the
// Makefile (`make check-scope`) and the lefthook pre-commit pipeline.
//
// It enforces that every hand-written .go file under internal/ and
// features/ carries a `// SCOPE:` annotation so template consumers can
// identify what is safe to remove. Test files, Templ output (*_templ.go)
// and any file carrying Go's standard generated-file marker
// (`// Code generated … DO NOT EDIT.`) are exempt — regenerating a file must
// never require hand-editing it.
//
// There is exactly ONE accepted annotation dialect — a single actionable axis:
//
//	// SCOPE:<core|plugin|feature> [— description]
//
//	core    — the binary does not work without it; never remove.
//	plugin  — the binary works but loses a capability.
//	feature — a demo/add-on user surface.
//
// The earlier two-axis `layer=…,removal=…` form was dropped: `layer` merely
// restated the directory (`internal/` = infra, `features/` = feature) and the
// two dialect variants drifted apart. `removal` is the axis that actually
// drives trimming, so it is the only one kept.
package main

import (
	"bufio"
	"fmt"
	"os"
	"path/filepath"
	"regexp"
	"strings"
)

const maxScanLines = 40

// canonical matches the single-axis form: one of the three removal classes at
// the start of the annotation. The boundary group stops a match inside a
// longer token (e.g. `corex`, `feature,removal=feature`).
var canonical = regexp.MustCompile(`^(core|plugin|feature)(\s|—|-|$)`)

// generatedMarker is Go's own generated-code convention (used by sqlc, stringer,
// mockgen, protoc, …). A file carrying it is not hand-written, so there is no
// human decision for a SCOPE annotation to record.
var generatedMarker = regexp.MustCompile(`^// Code generated .* DO NOT EDIT\.$`)

func main() {
	root := "."
	if len(os.Args) > 1 {
		root = os.Args[1]
	}

	offenders, err := collectOffenders(root)
	if err != nil {
		fmt.Fprintf(os.Stderr, "check-scope: %v\n", err)
		os.Exit(1)
	}

	if len(offenders) == 0 {
		fmt.Println("✅ SCOPE annotations present in internal/ + features/")
		return
	}
	report(offenders)
	os.Exit(2)
}

func collectOffenders(root string) ([]string, error) {
	var offenders []string

	// #nosec G703 -- local dev tool; root comes from the developer's own shell.
	err := filepath.WalkDir(root, func(path string, d os.DirEntry, err error) error {
		if err != nil {
			return err
		}
		if d.IsDir() {
			return skipRule(path, root, d.Name())
		}
		if !isLintable(path) || !inScopeDir(path) {
			return nil
		}
		ok, err := hasScopeAnnotation(path)
		if err != nil {
			return fmt.Errorf("%s: %w", path, err)
		}
		if !ok {
			offenders = append(offenders, path)
		}
		return nil
	})

	return offenders, err
}

func skipRule(path, root, name string) error {
	isRoot := filepath.Clean(path) == filepath.Clean(root)
	if !isRoot && (name == "vendor" || name == "tmp" || name == "node_modules" || strings.HasPrefix(name, ".")) {
		return filepath.SkipDir
	}
	return nil
}

func isLintable(path string) bool {
	base := filepath.Base(path)
	switch {
	case !strings.HasSuffix(path, ".go"):
		return false
	case strings.HasSuffix(base, "_templ.go"), strings.HasSuffix(base, "_test.go"):
		return false
	default:
		return true
	}
}

func inScopeDir(path string) bool {
	p := filepath.ToSlash(path)
	return strings.HasPrefix(p, "internal/") || strings.HasPrefix(p, "features/")
}

func hasScopeAnnotation(path string) (bool, error) {
	f, err := os.Open(path)
	if err != nil {
		return false, err
	}
	defer f.Close()

	sc := bufio.NewScanner(f)
	for i := 0; sc.Scan() && i < maxScanLines; i++ {
		raw := sc.Text()
		line := strings.TrimSpace(raw)
		switch {
		case line == "":
			continue
		case generatedMarker.MatchString(raw):
			return true, nil // machine-written; nothing to declare
		case strings.HasPrefix(raw, "package "):
			return false, nil // package clause reached without annotation
		case scopeFromLine(line):
			return true, nil
		}
	}
	return false, sc.Err()
}

func scopeFromLine(line string) bool {
	rest, ok := strings.CutPrefix(line, "//")
	if !ok {
		return false
	}
	rest = strings.TrimSpace(rest)
	body, ok := strings.CutPrefix(rest, "SCOPE:")
	if !ok {
		return false
	}
	return canonical.MatchString(strings.TrimSpace(body))
}

func report(offenders []string) {
	fmt.Fprintf(os.Stderr, "❌ %d file(s) missing '// SCOPE:' annotation:\n", len(offenders))
	for _, f := range offenders {
		fmt.Fprintf(os.Stderr, "  %s\n", f)
	}
	fmt.Fprintln(os.Stderr, "\nAdd the canonical annotation:")
	fmt.Fprintln(os.Stderr, "  // SCOPE:<core|plugin|feature> — why this file exists")
}
