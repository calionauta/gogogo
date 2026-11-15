// `recipe source`: read a recipe's code without installing it.
//
// The command exists so an agent can consult a slice and write its own, with no
// receipt and no copy. These tests pin the two properties that make it safe for
// that use: it reads the whole recipe (not the narrower reviewed surface) and it
// writes nothing.
package installer

import (
	"bytes"
	"encoding/json"
	"io"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"github.com/calionauta/gogogo/internal/recipe"
)

// sourceFixture builds a checkout with a receipt, a catalog entry and the
// recipe's files. The receipt lists TWO files while the catalog lists one, which
// is the shape that matters: the receipt is the full slice.
func sourceFixture(t *testing.T) string {
	t.Helper()
	root := t.TempDir()
	handlers := "package handlers\n\nconst X = 1\n"
	service := "package service\n\nconst Y = 2\n"
	writeFile(t, filepath.Join(root, "features/demo/handlers/chi.go"), handlers)
	writeFile(t, filepath.Join(root, "features/demo/service/s.go"), service)

	rec := recipe.Receipt{
		ID:      "demo",
		Version: "v1.0.0",
		Summary: "demo slice",
		Files: map[string]string{
			"features/demo/handlers/chi.go": recipe.Hash([]byte(handlers)),
			"features/demo/service/s.go":    recipe.Hash([]byte(service)),
		},
	}
	if err := recipe.SaveReceipt(root, rec); err != nil {
		t.Fatalf("write receipt: %v", err)
	}

	entry := "id: demo\nversion: v1.0.0\nsummary: demo slice\n" +
		"author: {name: fixture}\nlicense: MIT\nminCore: v0.62.0\nreleased: \"2026-11-08\"\n" +
		"source: {repo: github.com/example/demo, ref: abc123}\n" +
		"reviewedAt: \"2026-11-08\"\nreviewedBy: fixture\nreviewedAgainst: abc123\n" +
		"upstreamDeps: []\nfiles:\n  features/demo/service/s.go: \"" +
		recipe.Hash([]byte(service)) + "\"\n"
	writeFile(t, filepath.Join(root, "catalog/demo/v1.0.0.yaml"), entry)
	return root
}

// TestRecipeSourceReadsTheWholeReceipt is the difference from the catalog
// entry: the receipt enumerates every installed file, so the reader sees the
// slice, not just the reviewed surface.
func TestRecipeSourceReadsTheWholeReceipt(t *testing.T) {
	root := sourceFixture(t)
	var out bytes.Buffer
	if err := runRecipeSource([]string{"demo", "--dir", root, "--format", "json"}, &out); err != nil {
		t.Fatalf("recipe source: %v", err)
	}
	var rep sourceReport
	if err := json.Unmarshal(out.Bytes(), &rep); err != nil {
		t.Fatalf("decode: %v\n%s", err, out.String())
	}
	if !rep.ReadOnly {
		t.Error("the report must say it is read-only")
	}
	if len(rep.Files) != 2 {
		t.Fatalf("files = %d, want both receipt files:\n%s", len(rep.Files), out.String())
	}
	for _, f := range rep.Files {
		if !f.Pristine {
			t.Errorf("%s reported modified; the fixture is pristine", f.Path)
		}
		if f.Content == "" {
			t.Errorf("%s has no content", f.Path)
		}
	}
}

// TestRecipeSourceWritesNothing is the safety property: an agent may call this
// while deciding, and a write here would be a silent scaffold.
func TestRecipeSourceWritesNothing(t *testing.T) {
	root := sourceFixture(t)
	before := treeSnapshot(t, root)
	var out bytes.Buffer
	if err := runRecipeSource([]string{"demo", "--dir", root}, &out); err != nil {
		t.Fatalf("recipe source: %v", err)
	}
	after := treeSnapshot(t, root)
	if before != after {
		t.Fatalf("the tree changed:\nbefore:\n%s\nafter:\n%s", before, after)
	}
}

// TestRecipeSourceFiltersOneFile keeps the read small: an agent adapting one
// handler should not have to page through the slice.
func TestRecipeSourceFiltersOneFile(t *testing.T) {
	root := sourceFixture(t)
	var out bytes.Buffer
	if err := runRecipeSource([]string{"demo", "--dir", root, "--file", "handlers/chi.go"}, &out); err != nil {
		t.Fatalf("recipe source: %v", err)
	}
	text := out.String()
	if !strings.Contains(text, "features/demo/handlers/chi.go") {
		t.Errorf("the requested file is missing:\n%s", text)
	}
	if strings.Contains(text, "service/s.go") {
		t.Errorf("an unrequested file was printed:\n%s", text)
	}
}

// TestRecipeSourceReportsAModifiedFile: reading a fork is the normal case, and
// the reader needs to know the bytes are not the reviewed ones.
func TestRecipeSourceReportsAModifiedFile(t *testing.T) {
	root := sourceFixture(t)
	writeFile(t, filepath.Join(root, "features/demo/handlers/chi.go"), "package handlers\n\nconst X = 999\n")
	var out bytes.Buffer
	if err := runRecipeSource([]string{"demo", "--dir", root, "--format", "json"}, &out); err != nil {
		t.Fatalf("recipe source: %v", err)
	}
	var rep sourceReport
	if err := json.Unmarshal(out.Bytes(), &rep); err != nil {
		t.Fatal(err)
	}
	for _, f := range rep.Files {
		if strings.HasSuffix(f.Path, "handlers/chi.go") && f.Pristine {
			t.Error("a modified file was reported pristine")
		}
	}
}

// TestRecipeSourceWithoutAnIDPointsAtTheReferenceList: the first thing an agent
// (or a person) sees is a suggestion drawn from catalog/reference.yml, so the
// "which one?" question is answered, not just refused.
func TestRecipeSourceWithoutAnIDPointsAtTheReferenceList(t *testing.T) {
	var out bytes.Buffer
	err := runRecipeSource(nil, &out)
	if err == nil {
		t.Fatal("a bare `recipe source` was accepted")
	}
	if !strings.Contains(err.Error(), "todo") {
		t.Errorf("the error does not point at the reference slice: %v", err)
	}
}

// TestRecipeSourceExplainsADroppedSlice: after a scaffold drops a demo, the
// receipt is a tombstone and the catalog has no entry (it was never published).
// The error must say where the code still lives, not "no catalog entry".
func TestRecipeSourceExplainsADroppedSlice(t *testing.T) {
	root := sourceFixture(t)
	rec, loadErr := recipe.LoadReceipt(root, "demo")
	if loadErr != nil {
		t.Fatal(loadErr)
	}
	if err := rec.MarkRemoved(time.Now()); err != nil {
		t.Fatal(err)
	}
	if err := recipe.SaveReceipt(root, rec); err != nil {
		t.Fatal(err)
	}
	if err := os.Remove(filepath.Join(root, "catalog/demo/v1.0.0.yaml")); err != nil {
		t.Fatal(err)
	}
	var out bytes.Buffer
	err := runRecipeSource([]string{"demo", "--dir", root}, &out)
	if err == nil {
		t.Fatal("a dropped, uncatalogued slice was read")
	}
	if !strings.Contains(err.Error(), "removed from this tree") || !strings.Contains(err.Error(), "template") {
		t.Errorf("the error does not say where the code lives: %v", err)
	}
}

// TestRecipeSourceUnknownIDNamesTheAlternative ends a typo with a next step
// instead of a bare error.
func TestRecipeSourceUnknownIDNamesTheAlternative(t *testing.T) {
	root := sourceFixture(t)
	var out bytes.Buffer
	err := runRecipeSource([]string{"nope", "--dir", root}, &out)
	if err == nil {
		t.Fatal("an unknown id was accepted")
	}
	if !strings.Contains(err.Error(), "recipe list") {
		t.Errorf("the error does not point at `recipe list`: %v", err)
	}
}

// TestRecipeSourceReadsABundle covers the author's shape (--src), where the
// manifest is the only declaration and every file is pristine by definition.
func TestRecipeSourceReadsABundle(t *testing.T) {
	bundle := t.TempDir()
	writeFile(t, filepath.Join(bundle, ".recipe.yml"),
		"id: timer\nsummary: a timer\nversion: v1.0.0\n")
	writeFile(t, filepath.Join(bundle, "handlers/chi.go"), "package handlers\n")
	var out bytes.Buffer
	if err := runRecipeSource([]string{"--src", bundle, "--format", "json"}, &out); err != nil {
		t.Fatalf("recipe source: %v", err)
	}
	var rep sourceReport
	if err := json.Unmarshal(out.Bytes(), &rep); err != nil {
		t.Fatal(err)
	}
	if rep.ID != "timer" || len(rep.Files) != 1 || rep.Files[0].Path != "handlers/chi.go" {
		t.Fatalf("bundle report = %+v", rep)
	}
}

// treeSnapshot lists every path with its bytes, so a write anywhere is caught.
// It reads through os.Root: a snapshot helper that follows a symlink out of the
// fixture would report on a file the test never meant to cover.
func treeSnapshot(t *testing.T, root string) string {
	t.Helper()
	fsys, err := os.OpenRoot(root)
	if err != nil {
		t.Fatal(err)
	}
	defer func() { _ = fsys.Close() }()
	var b strings.Builder
	err = filepath.WalkDir(root, func(p string, d os.DirEntry, werr error) error {
		if werr != nil || d.IsDir() {
			return werr
		}
		rel, _ := filepath.Rel(root, p)
		f, oerr := fsys.Open(rel)
		if oerr != nil {
			return oerr
		}
		defer func() { _ = f.Close() }()
		raw, rerr := io.ReadAll(f)
		if rerr != nil {
			return rerr
		}
		b.WriteString(rel)
		b.WriteByte('=')
		b.Write(raw)
		b.WriteByte('\n')
		return nil
	})
	if err != nil {
		t.Fatal(err)
	}
	return b.String()
}

// TestRecipeSourceIgnoresARemovedReceipt: an uninstalled slice leaves a receipt
// that records what it OWNED. Its files are gone, so reading them would answer
// with empty content; the catalog entry is the honest source.
func TestRecipeSourceIgnoresARemovedReceipt(t *testing.T) {
	root := sourceFixture(t)
	rec, err := recipe.LoadReceipt(root, "demo")
	if err != nil {
		t.Fatal(err)
	}
	if err := rec.MarkRemoved(time.Now()); err != nil {
		t.Fatal(err)
	}
	if err := recipe.SaveReceipt(root, rec); err != nil {
		t.Fatal(err)
	}
	// The receipt listed two files; the catalog lists one. Reading one file
	// proves the catalog answered, not the tombstone.
	var out bytes.Buffer
	if err := runRecipeSource([]string{"demo", "--dir", root, "--format", "json"}, &out); err != nil {
		t.Fatalf("recipe source: %v", err)
	}
	var rep sourceReport
	if err := json.Unmarshal(out.Bytes(), &rep); err != nil {
		t.Fatal(err)
	}
	if len(rep.Files) != 1 || !strings.HasSuffix(rep.Files[0].Path, "service/s.go") {
		t.Fatalf("read from a removed receipt: %+v", rep.Files)
	}
}

// TestRecipeSourcePicksTheSemanticLatest: catalog versions are semver, so
// v1.10.0 is newer than v1.9.0 even though "v1.10.0" sorts first as text. The
// source command reuses the outdated check's ordering, and this pins it.
func TestRecipeSourcePicksTheSemanticLatest(t *testing.T) {
	root := t.TempDir()
	for _, v := range []string{"v1.9.0", "v1.10.0"} {
		writeFile(t, filepath.Join(root, "catalog", "demo", v+".yaml"), catalogEntryYAML("demo", v))
	}
	writeFile(t, filepath.Join(root, "features/demo/x.go"), "package demo\n")
	var out bytes.Buffer
	if err := runRecipeSource([]string{"demo", "--dir", root, "--format", "json"}, &out); err != nil {
		t.Fatalf("recipe source: %v", err)
	}
	var rep sourceReport
	if err := json.Unmarshal(out.Bytes(), &rep); err != nil {
		t.Fatal(err)
	}
	if rep.Version != "v1.10.0" {
		t.Errorf("picked %s, want the semantic latest v1.10.0", rep.Version)
	}
}

// catalogEntryYAML is a valid catalog entry (every required field), so the
// loader's validation is exercised instead of bypassed.
func catalogEntryYAML(id, version string) string {
	return "id: " + id + "\nversion: " + version + "\nsummary: fixture\n" +
		"author: {name: fixture}\nlicense: MIT\nminCore: v0.62.0\nreleased: \"2026-11-08\"\n" +
		"source: {repo: github.com/example/demo, ref: abc123}\n" +
		"reviewedAt: \"2026-11-08\"\nreviewedBy: fixture\nreviewedAgainst: abc123\n" +
		"upstreamDeps: []\nfiles:\n  features/demo/x.go: \"sha256:" + recipe.Hash([]byte("package demo\n")) + "\"\n"
}
