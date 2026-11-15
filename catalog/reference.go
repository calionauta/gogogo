// Package catalog is the reviewed set of recipes this template can install —
// entries plus the one list of which entries it recommends.
//
// It answers "what can be installed here, and what does the template advise?"
// in one place: `catalog/<id>/<version>.yaml` is the reviewed bytes, and
// `catalog/reference.yml` is the reading list. A separate `recipes/available/`
// directory used to hold bundle sources, which split the answer in two — it is
// gone. A bundle is now something you point `recipe install` at, not a second
// registry.
//
// It exists because the answer used to live in two unrelated places: a bool on
// the capability row (`Capability.Recommended`, consumed by the scaffold plan)
// and a `recommended:` field on each reviewed catalog entry (consumed by
// nothing that the plan reads). Two switches for one decision meant the plan
// could point at an id the catalog did not.
//
// The list is data (`reference.yml`), embedded so the binary carries the same
// list the checkout does. Removing an id here stops pointing at it; it changes
// nothing about what ships. This package only answers what to READ — the
// scaffold does not install anything from it, and the operator copies a slice
// explicitly with `recipe install`.
package catalog

import (
	_ "embed"
	"fmt"
	"strings"

	"gopkg.in/yaml.v3"
)

//go:embed reference.yml
var referenceYAML []byte

// Entry is one recommendation: the recipe id and the sentence shown to the
// operator. The sentence is the reason, not marketing copy — an id with no
// reason is a preference nobody can review.
type Entry struct {
	ID  string `yaml:"id"`
	Why string `yaml:"why"`
}

// ReferenceFile is where the list lives, repo-relative. It is exported so a
// gate can name the file it validated instead of restating the path.
const ReferenceFile = "catalog/reference.yml"

// referenceDoc mirrors the YAML document.
type referenceDoc struct {
	Recipes []Entry `yaml:"recipes"`
}

// Load parses the embedded list and validates it: every entry needs an id and
// a reason, and an id may appear once. A duplicate is the classic merge
// accident, and it would make the plan print the same advice twice.
func Reference() ([]Entry, error) {
	var doc referenceDoc
	if err := yaml.Unmarshal(referenceYAML, &doc); err != nil {
		return nil, fmt.Errorf("catalog: %s: %w", ReferenceFile, err)
	}
	seen := map[string]bool{}
	for i, e := range doc.Recipes {
		if strings.TrimSpace(e.ID) == "" {
			return nil, fmt.Errorf("catalog: %s: entry %d has no id", ReferenceFile, i+1)
		}
		if strings.TrimSpace(e.Why) == "" {
			return nil, fmt.Errorf("catalog: %s: %s has no why (a recommendation without a reason is unreviewable)",
				ReferenceFile, e.ID)
		}
		if seen[e.ID] {
			return nil, fmt.Errorf("catalog: %s: %s is listed twice", ReferenceFile, e.ID)
		}
		seen[e.ID] = true
	}
	return doc.Recipes, nil
}

// MustIDs is IDs for callers whose file is embedded: a malformed list is a
// broken tree, and the package test plus `make check-recipes` catch it before
// the binary ships, exactly like prompts.MustRender.
func MustReferenceIDs() []string {
	ids, err := ReferenceIDs()
	if err != nil {
		panic(err)
	}
	return ids
}

// ReferenceIDs returns the ids in file order. A caller that only points at the
// list does not have to unwind the reasons.
func ReferenceIDs() ([]string, error) {
	entries, err := Reference()
	if err != nil {
		return nil, err
	}
	out := make([]string, 0, len(entries))
	for _, e := range entries {
		out = append(out, e.ID)
	}
	return out, nil
}
