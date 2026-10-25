// SCOPE:core - DO NOT REMOVE - registry validation (part of make check-classes).
//
// The registry is the item 2026's design-system research measured as the
// biggest lever (CHI EA '26: registry-based assembly 95.08% compliance, ahead of
// putting the style guide in the prompt): an agent that assembles approved
// components beats an agent that re-derives UI from prose, and it beats one fed
// a long specification. For this repo the registry is a curated index — id,
// when, avoid, the canonical source, and the classes that must still be there.
//
// A registry nobody verifies rots exactly like the kit table did, so every entry
// is checked against the repo: the source file exists, every class it claims is
// still in that file, the doc anchor resolves, and every numbered composition in
// docs/ui-compositions.md has an entry (both directions, so a new composition
// cannot land undocumented and an entry cannot outlive its doc).
package main

import (
	"fmt"
	"os"
	"path/filepath"
	"regexp"
	"strings"

	"github.com/calionauta/daisyui-lint/daisyui"

	"github.com/calionauta/gogogo/internal/uiregistry"
)

// compositionsDoc is the human half of the registry.
const compositionsDoc = "docs/ui-compositions.md"

var entryIDRE = regexp.MustCompile(`^[a-z][a-z0-9-]+$`)

// registryFindings validates every entry against the repo and both directions
// against the compositions doc.
func registryFindings(root string, reg *uiregistry.Registry) []finding {
	var out []finding
	if reg.Version != uiregistry.CurrentVersion {
		out = append(out, registryFinding("version is %d, readers understand %d",
			reg.Version, uiregistry.CurrentVersion))
	}

	seen := map[string]bool{}
	anchored := map[string]bool{}
	for _, e := range reg.Entries {
		switch {
		case e.ID == "" || !entryIDRE.MatchString(e.ID):
			out = append(out, registryFinding("entry id %q must be a lowercase slug", e.ID))
		case seen[e.ID]:
			out = append(out, registryFinding("duplicate entry id %q", e.ID))
		}
		seen[e.ID] = true

		entryOut, anchor := entryFindings(root, e)
		out = append(out, entryOut...)
		if anchor != "" {
			anchored[anchor] = true
		}
	}

	// Both directions: a numbered composition in the doc needs an entry.
	for _, anchor := range uiregistry.CompositionAnchors(readUnder(root, compositionsDoc)) {
		key := compositionsDoc + "#" + anchor
		if !anchored[key] {
			out = append(out, registryFinding(
				"composition #%s in %s has no registry entry (add it to %s)",
				anchor, compositionsDoc, uiregistry.Path))
		}
	}
	return out
}

// entryFindings validates one entry: its prose, the markup it points at, and
// the doc anchor it claims. The second return is the resolved anchor key, or ""
// when the entry has no usable one.
func entryFindings(root string, e uiregistry.Entry) ([]finding, string) {
	out := missingFields(e)

	// The source must exist and still contain what the entry promises: this is
	// what makes the registry an index of REAL markup instead of a second,
	// drifting description of it. A class named in a comment does not count —
	// read the class attributes, the same extractor the gate uses.
	switch src := readUnder(root, e.Source); {
	case src != "":
		inMarkup := map[string]bool{}
		for _, tok := range daisyui.TokensInMarkup(src, e.Source) {
			inMarkup[tok.Name] = true
		}
		for _, class := range e.Shows {
			if !inMarkup[class] {
				out = append(out, registryFinding(
					"entry %q claims %q, which no class attribute in %s carries",
					e.ID, class, e.Source))
			}
		}
	case e.Source != "":
		out = append(out, registryFinding("entry %q points at %s, which does not exist", e.ID, e.Source))
	}

	// The doc half must resolve; its anchor is the link key used above.
	docFile, anchor, ok := strings.Cut(e.Doc, "#")
	if !ok || anchor == "" {
		return append(out, registryFinding("entry %q has no doc anchor (docs/file.md#heading)", e.ID)), ""
	}
	doc := readUnder(root, docFile)
	if doc == "" {
		return append(out, registryFinding("entry %q points at %s, which does not exist", e.ID, docFile)), ""
	}
	if !uiregistry.HasAnchor(doc, anchor) {
		return append(out, registryFinding(
			"entry %q anchor #%s does not resolve in %s", e.ID, anchor, docFile)), ""
	}
	return out, docFile + "#" + anchor
}

// missingFields reports the entry's empty required prose.
func missingFields(e uiregistry.Entry) []finding {
	var out []finding
	required := []struct {
		field string
		value string
	}{
		{"title", e.Title},
		{"when", e.When},
		{"avoid", e.Avoid},
		{"source", e.Source},
		{"doc", e.Doc},
	}
	for _, f := range required {
		if strings.TrimSpace(f.value) == "" {
			out = append(out, registryFinding("entry %q has no %s", e.ID, f.field))
		}
	}
	if len(e.Shows) == 0 {
		out = append(out, registryFinding("entry %q declares no classes (shows)", e.ID))
	}
	return out
}

func registryFinding(format string, args ...any) finding {
	return finding{file: uiregistry.Path, msg: fmt.Sprintf(format, args...)}
}

// readUnder returns a file under root, or "" when it cannot be read.
func readUnder(root, rel string) string {
	if rel == "" {
		return ""
	}
	// #nosec G703 -- local dev tool; rel comes from the registry the developer owns.
	raw, err := os.ReadFile(filepath.Join(root, filepath.FromSlash(rel)))
	if err != nil {
		return ""
	}
	return string(raw)
}
