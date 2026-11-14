package graph

import (
	"encoding/json"
	"errors"
	"os"
	"path/filepath"
	"slices"
	"strings"
	"testing"

	"github.com/calionauta/gogogo/internal/recipe"
)

func TestPanoramaIncludesCoreAndRecipes(t *testing.T) {
	var decl recipe.Declaration
	decl.Tables = []string{"ext_timer_sessions"}
	recipes := []Recipe{{ID: "timer", Declaration: decl}}
	doc := Panorama(recipes)
	if doc.Source != "panorama" {
		t.Fatalf("source = %q, want panorama", doc.Source)
	}
	var hasCore, hasRecipe, hasTable bool
	for _, n := range doc.Nodes {
		if n.Kind == "core" {
			hasCore = true
		}
		if n.ID == "recipe:timer" {
			hasRecipe = true
		}
		if n.ID == "ext_timer_sessions" {
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

// TestPanoramaKindsMatchTheCanvasVocabulary pins the WIRE values, not the Go
// identifiers: the canvas ships its own list (radius, colour, legend), so a
// class that changes spelling in the registry without the JS changing too
// would drop every node into the fallback ring and render an empty legend.
//
// The literals are written out on purpose: a rename must touch this test, the
// canvas and the registry together, which is the review you want.
func TestPanoramaKindsMatchTheCanvasVocabulary(t *testing.T) {
	want := []string{"core", "servant", "feature"}
	doc := Panorama(nil)
	seen := map[string]bool{}
	for _, n := range doc.Nodes {
		if n.Kind == "table" {
			continue
		}
		seen[n.Kind] = true
	}
	for _, kind := range want {
		if !seen[kind] {
			t.Errorf("no node carries kind %q — the canvas legend would render an empty class", kind)
		}
	}
	for kind := range seen {
		if !slices.Contains(want, kind) {
			t.Errorf("node kind %q is not in the canvas vocabulary %v", kind, want)
		}
	}
}

// TestCanvasKnowsEveryClass reads the shipped script: layout, colours and the
// legend all switch on the kind string, so a class missing there is a silently
// un-drawable node rather than a build error.
func TestCanvasKnowsEveryClass(t *testing.T) {
	root, err := moduleRoot()
	if err != nil {
		t.Fatalf("moduleRoot: %v", err)
	}
	raw, err := os.ReadFile(filepath.Join(root, "features", "graph", "static", "graph.js"))
	if err != nil {
		t.Fatalf("read graph.js: %v", err)
	}
	script := string(raw)
	for _, kind := range []string{"core", "servant", "feature"} {
		if !strings.Contains(script, "'"+kind+"'") {
			t.Errorf("graph.js does not mention kind %q — the canvas would fall back silently", kind)
		}
	}
}

// moduleRoot finds the checkout root from the test's working directory.
func moduleRoot() (string, error) {
	dir, err := os.Getwd()
	if err != nil {
		return "", err
	}
	for range 6 {
		if _, statErr := os.Stat(filepath.Join(dir, "go.mod")); statErr == nil {
			return dir, nil
		}
		dir = filepath.Dir(dir)
	}
	return "", errors.New("go.mod not found above the test directory")
}
