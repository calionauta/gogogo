// `recipe receipts`: do the declared hashes still describe the tree?
//
// This is the gate that would have caught 82 stale hashes sitting in the
// template's own receipts. It exists separately from `recipe check` because a
// `modified` file is a fork's edit and must not fail there.
package installer

import (
	"bytes"
	"encoding/json"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"github.com/calionauta/gogogo/internal/recipe"
)

// receiptsFixture writes one recipe whose receipt matches its file.
func receiptsFixture(t *testing.T) (root string) {
	t.Helper()
	root = t.TempDir()
	writeFile(t, filepath.Join(root, "cmd/web/main.go"), "package main\n")
	writeFile(t, filepath.Join(root, "internal/installer/run.go"), "package installer\n")
	body := "package demo\n"
	writeFile(t, filepath.Join(root, "features/demo/x.go"), body)
	if err := recipe.SaveReceipt(root, recipe.Receipt{
		ID:    "demo",
		Files: map[string]string{"features/demo/x.go": recipe.Hash([]byte(body))},
	}); err != nil {
		t.Fatal(err)
	}
	return root
}

func TestReceiptsPassWhenHashesMatch(t *testing.T) {
	var out bytes.Buffer
	if err := runRecipeReceipts([]string{"--dir", receiptsFixture(t)}, &out); err != nil {
		t.Fatalf("a matching receipt failed: %v\n%s", err, out.String())
	}
	if !strings.Contains(out.String(), "matches the tree") {
		t.Errorf("no verdict printed:\n%s", out.String())
	}
}

// TestReceiptsReportAStaleHash is the failure the template shipped: the file
// changed, the receipt did not, and nothing said so.
func TestReceiptsReportAStaleHash(t *testing.T) {
	root := receiptsFixture(t)
	writeFile(t, filepath.Join(root, "features/demo/x.go"), "package demo\n\n// edited\n")
	var out bytes.Buffer
	err := runRecipeReceipts([]string{"--dir", root}, &out)
	if err == nil {
		t.Fatal("a stale hash passed the gate")
	}
	if !strings.Contains(out.String(), "features/demo/x.go") {
		t.Errorf("the stale file is not named:\n%s", out.String())
	}
	if !strings.Contains(err.Error(), "--write") {
		t.Errorf("the error does not name the fix: %v", err)
	}
}

func TestReceiptsWriteFixesInPlace(t *testing.T) {
	root := receiptsFixture(t)
	edited := "package demo\n\n// edited\n"
	writeFile(t, filepath.Join(root, "features/demo/x.go"), edited)

	var out bytes.Buffer
	if err := runRecipeReceipts([]string{"--dir", root, "--write"}, &out); err != nil {
		t.Fatalf("--write: %v\n%s", err, out.String())
	}
	// The fix is the RECORD, not the file: the edited bytes stay.
	if got, _ := os.ReadFile(filepath.Join(root, "features/demo/x.go")); string(got) != edited {
		t.Errorf("--write touched the source file: %q", got)
	}
	r, err := recipe.LoadReceipt(root, "demo")
	if err != nil {
		t.Fatal(err)
	}
	if r.Files["features/demo/x.go"] != recipe.Hash([]byte(edited)) {
		t.Error("--write did not record the new hash")
	}
	out.Reset()
	if err := runRecipeReceipts([]string{"--dir", root}, &out); err != nil {
		t.Fatalf("after --write the gate is still red: %v", err)
	}
}

// TestReceiptsIgnoreADeletedFile: a missing file is a removal, which
// `recipe status` reports and this verb must not re-hash into existence.
func TestReceiptsIgnoreADeletedFile(t *testing.T) {
	root := receiptsFixture(t)
	if err := os.Remove(filepath.Join(root, "features/demo/x.go")); err != nil {
		t.Fatal(err)
	}
	if err := runRecipeReceipts([]string{"--dir", root}, &bytes.Buffer{}); err != nil {
		t.Fatalf("a deletion was reported as drift: %v", err)
	}
}

// TestReceiptsSkipTombstones: a removed receipt describes data ownership, not
// bytes, so its hashes are allowed to be stale.
func TestReceiptsSkipTombstones(t *testing.T) {
	root := receiptsFixture(t)
	r, err := recipe.LoadReceipt(root, "demo")
	if err != nil {
		t.Fatal(err)
	}
	if err := r.MarkRemoved(time.Now()); err != nil {
		t.Fatal(err)
	}
	if err := recipe.SaveReceipt(root, r); err != nil {
		t.Fatal(err)
	}
	writeFile(t, filepath.Join(root, "features/demo/x.go"), "package demo\n\n// gone from the tree\n")
	if err := runRecipeReceipts([]string{"--dir", root}, &bytes.Buffer{}); err != nil {
		t.Fatalf("a tombstone was graded as drift: %v", err)
	}
}

func TestReceiptsJSONIsTheMachineShape(t *testing.T) {
	root := receiptsFixture(t)
	writeFile(t, filepath.Join(root, "features/demo/x.go"), "package demo\n\n// edited\n")
	var out bytes.Buffer
	_ = runRecipeReceipts([]string{"--dir", root, "--format", "json"}, &out)
	var rows []receiptDriftRow
	if err := json.Unmarshal(out.Bytes(), &rows); err != nil {
		t.Fatalf("decode: %v\n%s", err, out.String())
	}
	if len(rows) != 1 || rows[0].ID != "demo" || len(rows[0].Stale) != 1 {
		t.Fatalf("rows = %+v", rows)
	}
}

// TestReceiptsWriteHandlesQuotedAndBareDigests: a receipt may spell its digest
// either way — `recipe.SaveReceipt` writes it bare, a hand-written or older one
// quotes it — and a fix that only knew one spelling silently skipped the other.
func TestReceiptsWriteHandlesQuotedAndBareDigests(t *testing.T) {
	for _, style := range []struct{ name, tmpl string }{
		{"bare", "id: demo\nfiles:\n  features/demo/x.go: %s\n"},
		{"quoted", "id: demo\nfiles:\n  features/demo/x.go: \"%s\"\n"},
	} {
		t.Run(style.name, func(t *testing.T) {
			root := t.TempDir()
			writeFile(t, filepath.Join(root, "cmd/web/main.go"), "package main\n")
			writeFile(t, filepath.Join(root, "features/demo/x.go"), "package demo\n")
			writeFile(t, filepath.Join(root, ".recipes/demo.yml"),
				strings.Replace(style.tmpl, "%s", strings.Repeat("0", 64), 1))

			var out bytes.Buffer
			if err := runRecipeReceipts([]string{"--dir", root, "--write"}, &out); err != nil {
				t.Fatalf("--write: %v\n%s", err, out.String())
			}
			if err := runRecipeReceipts([]string{"--dir", root}, &bytes.Buffer{}); err != nil {
				t.Fatalf("%s digest was not fixed: %v", style.name, err)
			}
		})
	}
}

// TestRenameRehashesReceipts is the bug a fresh scaffold hit: the rename
// rewrites every `.go`/`.templ` import, so every recorded hash stops describing
// its file — and the product's `make check-recipes` is red before anyone edits a
// line. The template itself had rotted this way in 82 of 276 files.
func TestRenameRehashesReceipts(t *testing.T) {
	root := t.TempDir()
	writeFile(t, filepath.Join(root, "go.mod"), "module github.com/calionauta/gogogo\n\ngo 1.27\n")
	writeFile(t, filepath.Join(root, "cmd/gogogo/main.go"), "package main\n")
	// looksLikeTemplate probes these two; a rename moves cmd/gogogo.
	writeFile(t, filepath.Join(root, "internal/installer/run.go"), "package installer\n")
	body := "package demo\n\nimport \"github.com/calionauta/gogogo/internal/recipe\"\n\nvar _ recipe.Receipt\n"
	writeFile(t, filepath.Join(root, "features/demo/x.go"), body)
	if err := recipe.SaveReceipt(root, recipe.Receipt{
		ID:    "demo",
		Files: map[string]string{"features/demo/x.go": recipe.Hash([]byte(body))},
	}); err != nil {
		t.Fatal(err)
	}

	if err := renameTree(root, "my-app", "me"); err != nil {
		t.Fatalf("renameTree: %v", err)
	}
	var out bytes.Buffer
	if err := runRecipeReceipts([]string{"--dir", root}, &out); err != nil {
		t.Fatalf("the rename left the receipt stale: %v\n%s", err, out.String())
	}
	// And the recorded hash is the rewritten file's, not the old body's.
	r, err := recipe.LoadReceipt(root, "demo")
	if err != nil {
		t.Fatal(err)
	}
	got, _ := os.ReadFile(filepath.Join(root, "features/demo/x.go"))
	if r.Files["features/demo/x.go"] != recipe.Hash(got) {
		t.Error("the receipt does not describe the renamed file")
	}
}
