// SCOPE:layer=infra,removal=plugin — installer engine: add span insertion and anchoring
//
// Span insertion and anchoring: putting a trimmed span back where it came
// from, by locating the anchor line the strip rule recorded.
package installer

import (
	"fmt"
	"os"
	"path/filepath"
	"slices"
	"strings"
)

// insertRuleSpan copies one rule's span from source into target at anchor.
func insertRuleSpan(from, root, path string, r stripRule, anchor spanAnchor, rc *AddReceipt) (int, error) {
	return insertSpan(from, root, path, path, []stripRule{r}, anchor, rc)
}

// spanAnchor locates an insertion point: before/after the first line
// exactly equal (or, for navLinkAnchor, containing) the marker.
type spanAnchor struct {
	before string
	after  string
	// afterContains matches by substring (nav links carry classes).
	afterContains string
}

func anchorBefore(line string) spanAnchor { return spanAnchor{before: line} }
func anchorAfter(line string) spanAnchor  { return spanAnchor{after: line} }

// templSpanAnchor maps edited .templ/navbar/handler files to anchors.
// Files without an entry (sounds call sites, brand) stay manual by design.
func templSpanAnchor(path string) (spanAnchor, bool) {
	switch path {
	case navbarTempl:
		return spanAnchor{afterContains: navLinkAnchorSubstr}, true
	case "features/todo/handlers/todo.go":
		return anchorBefore(todoLayoutAnchor), true
	case "features/todo/handlers/todo_repo.go":
		return anchorBefore(todoRegionAnchor), true
	default:
		return spanAnchor{}, false
	}
}

// insertSpan copies the lines covered by rules from the source file into
// the target file at anchor. Fully present spans are skipped (idempotent).
func insertSpan(
	from, root, srcPath, dstPath string,
	rules []stripRule, anchor spanAnchor, rc *AddReceipt,
) (int, error) {
	srcRaw, err := os.ReadFile(filepath.Join(from, filepath.FromSlash(srcPath)))
	if err != nil {
		return 0, fmt.Errorf("template source missing %s (pass --from a pristine checkout)", srcPath)
	}
	srcLines := strings.Split(string(srcRaw), "\n")
	dst := filepath.Join(root, filepath.FromSlash(dstPath))
	dstRaw, err := os.ReadFile(dst)
	if err != nil {
		return 0, fmt.Errorf("target missing %s (not a scaffolded checkout?)", dstPath)
	}
	dstLines := strings.Split(string(dstRaw), "\n")
	inserted := 0
	for _, r := range rules {
		span := extractSpan(srcLines, r)
		if len(span) == 0 {
			return inserted, fmt.Errorf("template source %s lost marker %q (drifted template?)", srcPath, r.startMarker)
		}
		if spanPresent(dstLines, span) {
			continue
		}
		at, err := findAnchor(dstLines, anchor)
		if err != nil {
			return inserted, fmt.Errorf("%s: %w", dstPath, err)
		}
		dstLines = append(dstLines[:at], append(slices.Clone(span), dstLines[at:]...)...)
		inserted += len(span)
	}
	if inserted == 0 {
		return 0, nil
	}
	//nolint:gosec // G306 scaffolded repo files are 0644 tracked sources, same as a git checkout.
	if err := os.WriteFile(dst, []byte(strings.Join(dstLines, "\n")), scaffoldFileMode); err != nil {
		return inserted, err
	}
	rc.touch(filepath.ToSlash(dstPath))
	return inserted, nil
}

// extractSpan returns the source lines a strip rule would delete.
func extractSpan(srcLines []string, r stripRule) []string {
	for i := range len(srcLines) {
		if !strings.Contains(srcLines[i], r.startMarker) {
			continue
		}
		end := stripEnd(srcLines, i, r)
		if end == -1 {
			return nil
		}
		return slices.Clone(srcLines[i : end+1])
	}
	return nil
}

// spanPresent reports whether every span line already exists in target.
func spanPresent(dstLines, span []string) bool {
	set := make(map[string]bool, len(dstLines))
	for _, l := range dstLines {
		set[l] = true
	}
	for _, l := range span {
		if !set[l] {
			return false
		}
	}
	return true
}

func findAnchor(dstLines []string, anchor spanAnchor) (int, error) {
	for i, l := range dstLines {
		switch {
		case anchor.before != "" && l == anchor.before:
			return i, nil
		case anchor.after != "" && l == anchor.after:
			return i + 1, nil
		case anchor.afterContains != "" && strings.Contains(l, anchor.afterContains):
			return i + 1, nil
		}
	}
	return -1, fmt.Errorf("cannot locate insertion anchor (evolved file?)")
}
