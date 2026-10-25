// SCOPE:core — the UI registry schema, shared by the gate and the reader.
//
// One schema, two consumers: `make check-classes` validates the registry
// against the repo, and `go run ./cmd/ui-registry` serves it to an agent that
// is about to build UI. Keeping the structs and the anchor rules here means the
// reader can never disagree with the checker about what a valid entry is.
package uiregistry

import (
	"encoding/json"
	"fmt"
	"os"
	"path/filepath"
	"regexp"
	"slices"
	"sort"
	"strings"
)

// Path is the registry's location, relative to the repo root.
const Path = "internal/ui/registry.json"

// Registry is the file format. Version is the schema's own version, checked by
// the gate so a future format change cannot be read by old eyes.
type Registry struct {
	Version int     `json:"version"`
	Note    string  `json:"note"`
	Entries []Entry `json:"entries"`
}

// CurrentVersion is the only schema the readers understand.
const CurrentVersion = 1

// Entry is one approved composition.
type Entry struct {
	ID     string   `json:"id"`
	Title  string   `json:"title"`
	When   string   `json:"when"`
	Avoid  string   `json:"avoid"`
	Source string   `json:"source"`
	Shows  []string `json:"shows"`
	Doc    string   `json:"doc"`
}

// Load reads and parses the registry under root.
func Load(root string) (*Registry, error) {
	// #nosec G703 -- local dev tool; the path is a package constant under root.
	raw, err := os.ReadFile(filepath.Join(root, filepath.FromSlash(Path)))
	if err != nil {
		return nil, fmt.Errorf("read %s: %w", Path, err)
	}
	var reg Registry
	if err := json.Unmarshal(raw, &reg); err != nil {
		return nil, fmt.Errorf("parse %s: %w", Path, err)
	}
	return &reg, nil
}

// Find returns the entry with this id.
func (r *Registry) Find(id string) (Entry, bool) {
	for _, e := range r.Entries {
		if e.ID == id {
			return e, true
		}
	}
	return Entry{}, false
}

// headingRE matches the Markdown headings an anchor can point at.
var headingRE = regexp.MustCompile(`(?m)^#{2,3} (.+)$`)

// HasAnchor reports whether a heading in doc slugs to anchor.
func HasAnchor(doc, anchor string) bool {
	return slices.Contains(Anchors(doc), anchor)
}

// Anchors lists every heading anchor in a document.
func Anchors(doc string) []string {
	matches := headingRE.FindAllStringSubmatch(doc, -1)
	out := make([]string, 0, len(matches))
	for _, m := range matches {
		out = append(out, Slug(m[1]))
	}
	return out
}

// CompositionAnchors lists the numbered composition headings (`## 1. …`). The
// trailing prose section ("Why these and not others") is not a composition.
func CompositionAnchors(doc string) []string {
	var out []string
	for _, m := range headingRE.FindAllStringSubmatch(doc, -1) {
		title := m[1]
		if title == "" || title[0] < '0' || title[0] > '9' {
			continue
		}
		out = append(out, Slug(title))
	}
	sort.Strings(out)
	return out
}

// Slug mirrors GitHub's heading anchors: lowercase, apostrophes dropped, other
// punctuation and spaces become single hyphens.
func Slug(s string) string {
	var b strings.Builder
	gap := false
	for _, r := range strings.ToLower(s) {
		switch {
		case r == '\'' || r == '\u2019':
			// GitHub drops apostrophes ("panel's" -> "panels").
		case r >= 'a' && r <= 'z', r >= '0' && r <= '9', r == '-':
			if gap && b.Len() > 0 {
				b.WriteByte('-')
			}
			gap = false
			b.WriteRune(r)
		default:
			gap = true
		}
	}
	return b.String()
}
