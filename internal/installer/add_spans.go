// SCOPE:plugin — installer engine: add span insertion and anchoring
//
// Span insertion and anchoring: putting a trimmed span back where it came
// from, by locating the anchor line the strip rule recorded.
package installer

import (
	"errors"
	"fmt"
	"io"
	"os"
	"path/filepath"
	"slices"
	"strings"
)

// insertRuleSpan copies one rule's span from source into target at anchor.
func insertRuleSpan(t *treeFS, from, root, path string, r stripRule, anchor spanAnchor, rc *AddReceipt) (int, error) {
	return insertSpan(t, from, root, path, path, []stripRule{r}, anchor, rc)
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

// addSpanAnchor resolves where a strip span is re-inserted: main.go after
// the shutdown defer, desktop before the Wails boot section, .templ/handler
// files from templSpanAnchor. A rule's own addBefore/addAfter wins over the
// path default (checked by the caller). Empty means the path is outside the
// add convention — trim may strip it, add cannot restore it.
func addSpanAnchor(path string) spanAnchor {
	switch path {
	case filepath.Join("cmd", "web", "main.go"):
		return anchorAfter(mainCallsAnchor)
	case filepath.Join("cmd", "desktop", "main.go"):
		return anchorBefore(desktopBlockAnchor)
	default:
		if anchor, ok := templSpanAnchor(path); ok {
			return anchor
		}
		return spanAnchor{}
	}
}

// unitPathsPresent reports whether a unit's owned paths all exist in the
// target tree: re-adding it restores nothing, so the reversibility gate
// (and the copy step) skips it.
func unitPathsPresent(root string, u trimUnit) bool {
	m := u.meta()
	for _, d := range m.dirs {
		if _, err := os.Stat(filepath.Join(root, filepath.FromSlash(d))); err != nil {
			return false
		}
	}
	for _, f := range m.files {
		if _, err := os.Stat(filepath.Join(root, filepath.FromSlash(f))); err != nil {
			return false
		}
	}
	return len(m.dirs)+len(m.files) > 0
}

// addManualWiring lists the trim edits `add` cannot reverse mechanically,
// one repo-relative path + dropped line each. Restorable today: mux.Handle
// call lines in chi.go (reinserted at the mux anchor), import lines
// anywhere (merged into import blocks only when used), strips on anchored
// paths, main/desktop strips, and go.mod drops (tidy re-adds). Everything
// else — constructions, call args, signature params, test bodies, one-way
// replaces, strips on unanchored paths — needs a human (or an LLM with the
// exact lines below, which is why they are printed, not summarized).
// Non-empty means add would leave a broken tree: callers refuse with this
// recipe instead of writing code the proof then rejects.
func addManualWiring(u trimUnit) []string {
	var out []string
	for _, ed := range u.extraDrops {
		for _, sub := range ed.substrs {
			if ed.path == chiGoFile && strings.HasPrefix(strings.TrimSpace(sub), "mux.Handle") {
				continue
			}
			if strings.HasSuffix(sub, `"`) {
				continue
			}
			out = append(out, ed.path+": "+sub)
		}
	}
	for _, es := range u.extraStrips {
		restorable := addSpanAnchor(es.path) != (spanAnchor{})
		if !restorable {
			for _, r := range es.rules {
				if r.addBefore != "" || r.addAfter != "" {
					restorable = true
					break
				}
			}
		}
		if !restorable {
			for _, r := range es.rules {
				out = append(out, es.path+": block "+r.startMarker)
			}
		}
	}
	for _, rp := range u.replaces {
		out = append(out, rp.path+": one-way replace "+rp.old)
	}
	return out
}

// templSpanAnchor maps edited .templ/navbar/handler files to anchors.
// Files without an entry (sounds call sites, brand) stay manual by design.
func templSpanAnchor(path string) (spanAnchor, bool) {
	switch path {
	case navbarTempl:
		return spanAnchor{afterContains: navLinkAnchorSubstr}, true
	case todoGoFile:
		return anchorBefore(todoLayoutAnchor), true
	case todoRepoFile:
		return anchorBefore(todoRegionAnchor), true
	default:
		return spanAnchor{}, false
	}
}

// insertSpan copies the lines covered by rules from the source file into
// the target file at anchor. Fully present spans are skipped (idempotent).
func insertSpan(
	t *treeFS, from, root, srcPath, dstPath string,
	rules []stripRule, anchor spanAnchor, rc *AddReceipt,
) (int, error) {
	srcRaw, err := os.ReadFile(filepath.Join(from, filepath.FromSlash(srcPath)))
	if err != nil {
		return 0, fmt.Errorf("template source missing %s (pass --from a pristine checkout)", srcPath)
	}
	srcLines := strings.Split(string(srcRaw), "\n")
	dst := filepath.Join(root, filepath.FromSlash(dstPath))
	dstRaw, err := t.ReadFile(dst)
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
	if err := t.WriteFile(dst, []byte(strings.Join(dstLines, "\n")), scaffoldFileMode); err != nil {
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
	return -1, errors.New("cannot locate insertion anchor (evolved file?)")
}

// refuseIrreversible gates executeAdd: units whose trim edits add cannot
// replay mechanically refuse here — before any mutation — with the exact
// lines to re-add by hand plus the unit's follow-ups. Writing the
// reversible subset and failing the proof mid-tree is worse than
// refusing: a half-rewired checkout reads as broken either way, but only
// the refusal leaves the tree untouched. Fully present units skip the
// gate (idempotent re-add restores nothing). Split out of executeAdd for
// the gocyclo gate.
func refuseIrreversible(stdout io.Writer, opt addOptions) error {
	for _, u := range opt.units {
		if unitPathsPresent(opt.dir, u) {
			continue
		}
		if manual := addManualWiring(u); len(manual) > 0 {
			for _, line := range manual {
				fmt.Fprintf(stdout, "gogogo: manual: %s\n", line)
			}
			for _, warn := range addFollowUps(u) {
				fmt.Fprintf(stdout, "gogogo: manual follow-up: %s\n", warn)
			}
			return &ExitError{code: 1, msg: fmt.Sprintf(
				"add %s is not fully reversible: re-add the %d line(s) above by hand "+
					"(positions mirror the trim strips), then run `go build ./...`",
				u.id, len(manual))}
		}
	}
	return nil
}
