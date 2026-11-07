package graph

import (
	"encoding/json"
	"testing"

	"github.com/calionauta/gogogo/internal/recipe"
)

func TestPanoramaIncludesCoreAndRecipes(t *testing.T) {
	var decl recipe.Declaration
	decl.Tables = []string{"ext_pomodoro_sessions"}
	recipes := []Recipe{{ID: "pomodoro", Declaration: decl}}
	doc := Panorama(recipes)
	if doc.Source != "panorama" {
		t.Fatalf("source = %q, want panorama", doc.Source)
	}
	var hasCore, hasRecipe, hasTable bool
	for _, n := range doc.Nodes {
		if n.Kind == "core" {
			hasCore = true
		}
		if n.ID == "recipe:pomodoro" {
			hasRecipe = true
		}
		if n.ID == "ext_pomodoro_sessions" {
			hasTable = true
		}
	}
	if !hasCore || !hasRecipe || !hasTable {
		t.Fatalf("panorama missing kinds (core=%v recipe=%v table=%v) nodes=%d", hasCore, hasRecipe, hasTable, len(doc.Nodes))
	}
	// Deterministic: same input, same positions.
	a := Panorama(recipes)
	b := Panorama(recipes)
	if len(a.Nodes) != len(b.Nodes) {
		t.Fatalf("nondeterministic node count")
	}
	for i := range a.Nodes {
		if a.Nodes[i].X != b.Nodes[i].X || a.Nodes[i].Y != b.Nodes[i].Y {
			t.Fatalf("nondeterministic layout at %d", i)
		}
	}
}

func TestPanoramaDocumentEncodes(t *testing.T) {
	doc := Panorama(nil)
	data, err := json.Marshal(doc)
	if err != nil {
		t.Fatal(err)
	}
	var back Document
	if err := json.Unmarshal(data, &back); err != nil {
		t.Fatal(err)
	}
	if len(back.Nodes) == 0 {
		t.Fatal("empty panorama")
	}
}
