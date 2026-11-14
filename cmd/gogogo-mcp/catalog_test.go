package main

import (
	"strings"
	"testing"
)

// TestCatalogDescribesEveryTool pins the catalog's own shape. The catalog is
// the served surface AND the source the documentation tables are generated
// from, so a tool without a description, a parameter summary or a safety
// verdict is a hole in both.
func TestCatalogDescribesEveryTool(t *testing.T) {
	seen := map[string]bool{}
	for _, d := range toolCatalog {
		if d.name == "" || d.desc == "" || d.params == "" || d.effect == "" {
			t.Errorf("tool %q is under-described: name=%q params=%q effect=%q",
				d.name, d.name, d.params, d.effect)
		}
		if seen[d.name] {
			t.Errorf("duplicate tool name %q", d.name)
		}
		seen[d.name] = true
	}
}

// TestCatalogEffectMatchesTheConfirmGate pins the safety column to the guard.
// A tool that takes `confirm:true` mutates the tree; a tool whose effect
// claims it mutates must ask for that confirmation. Without this pairing the
// docs table could say "No" about a tool that rewrites the checkout — the one
// fact a reader of the MCP page must be able to trust.
func TestCatalogEffectMatchesTheConfirmGate(t *testing.T) {
	for _, d := range toolCatalog {
		takesConfirm := strings.Contains(d.params, "confirm")
		claimsMutating := strings.HasPrefix(d.effect, "Yes")
		if takesConfirm != claimsMutating {
			t.Errorf("tool %q: params %q vs effect %q — a tool that mutates must take confirm:true, and one that takes it must say so",
				d.name, d.params, d.effect)
		}
	}
}

// TestMutatingToolsAreTheKnownSet pins WHICH tools mutate. The set is small
// and known: the scaffolder, the recipe lifecycle, and nothing else. A new
// mutating tool must show up here, in a review, not in a docs table nobody
// read.
func TestMutatingToolsAreTheKnownSet(t *testing.T) {
	mutating := map[string]bool{}
	for _, d := range toolCatalog {
		if strings.HasPrefix(d.effect, "Yes") {
			mutating[d.name] = true
		}
	}
	want := map[string]bool{
		"scaffold_apply":   true,
		"new_feature":      true,
		"recipe_install":   true,
		"recipe_update":    true,
		"recipe_uninstall": true,
	}
	for name := range want {
		if !mutating[name] {
			t.Errorf("%q must be declared as mutating", name)
		}
		delete(mutating, name)
	}
	for name := range mutating {
		t.Errorf("%q is declared as mutating but is not in the known set", name)
	}
}
