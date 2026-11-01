// SCOPE:plugin — Spike 3 evidence: install and remove are mechanical.
//
// The binary criterion: an install writes the copied tree plus the receipt and
// nothing else, and a remove deletes only what the user never touched.
package recipe

import (
	"os"
	"path/filepath"
	"strings"
	"testing"
)

func readOrEmpty(t *testing.T, root, rel string) string {
	t.Helper()
	raw, err := os.ReadFile(filepath.Join(root, filepath.FromSlash(rel)))
	if err != nil {
		return ""
	}
	return string(raw)
}

// TestInstallWritesTreeAndReceiptOnly is the criterion in miniature: after an
// install, the files that exist are exactly the recipe's files plus its receipt.
func TestInstallWritesTreeAndReceiptOnly(t *testing.T) {
	root := t.TempDir()
	up := map[string]string{
		"features/pomodoro/page.go":   pageSrc,
		"features/pomodoro/domain.go": "package pomodoro\n\nfunc Timer() {}\n",
	}
	plan := Plan(Receipt{Files: map[string]string{}}, up, func(string) (string, bool) { return "", false })
	res, err := Apply(root, plan)
	if err != nil {
		t.Fatal(err)
	}
	if len(res.Written) != 2 {
		t.Fatalf("expected 2 files written, got %v", res.Written)
	}

	written := map[string]string{}
	for _, p := range res.Written {
		written[p] = up[p]
	}
	rec := NewReceipt("pomodoro", Upstream{Repo: "github.com/calionauta/gogogo", Ref: "v0.56.0"},
		Declaration{Routes: []Route{{Method: "GET", Pattern: "/pomodoro"}}}, written)
	if serr := SaveReceipt(root, rec); serr != nil {
		t.Fatal(serr)
	}

	got, err := ListReceipts(root)
	if err != nil {
		t.Fatal(err)
	}
	if len(got) != 1 || got[0] != "pomodoro" {
		t.Fatalf("expected the recipe to be enumerable, got %v", got)
	}

	// A fresh install must leave the tree pristine by its own receipt: that is
	// what makes the first update a fast-forward.
	d, err := Check(root, rec)
	if err != nil {
		t.Fatal(err)
	}
	if !d.Pristine() {
		t.Fatalf("a fresh install must be pristine, got %+v", d.Files)
	}
}

// TestRemoveDeletesOnlyPristineFiles is the property that makes uninstall safe: a
// file the user wrote is never deleted, and the report says so.
func TestRemoveDeletesOnlyPristineFiles(t *testing.T) {
	root := t.TempDir()
	up := map[string]string{
		"features/pomodoro/page.go":   pageSrc,
		"features/pomodoro/domain.go": "package pomodoro\n\nfunc Timer() {}\n",
	}
	plan := Plan(Receipt{Files: map[string]string{}}, up, func(string) (string, bool) { return "", false })
	if _, err := Apply(root, plan); err != nil {
		t.Fatal(err)
	}
	rec := NewReceipt("pomodoro", Upstream{}, Declaration{}, up)
	if err := SaveReceipt(root, rec); err != nil {
		t.Fatal(err)
	}

	// The user rewrites one file.
	mine := "package pomodoro\n\nfunc Timer() { /* mine, keep it */ }\n"
	if err := os.WriteFile(filepath.Join(root, "features/pomodoro/domain.go"), []byte(mine), filePerm); err != nil {
		t.Fatal(err)
	}

	res, err := Remove(root, rec)
	if err != nil {
		t.Fatal(err)
	}
	if got := readOrEmpty(t, root, "features/pomodoro/domain.go"); got != mine {
		t.Fatalf("a modified file must survive uninstall, got %q", got)
	}
	if got := readOrEmpty(t, root, "features/pomodoro/page.go"); got != "" {
		t.Fatal("a pristine file must be removed")
	}
	if len(res.Skipped) != 1 {
		t.Fatalf("the kept file must be reported, got %v", res.Skipped)
	}
}

// TestApplyRefusesABlockedPlan is the safety property that matters most: a plan
// with a conflict is applied as nothing at all, never partially.
func TestApplyRefusesABlockedPlan(t *testing.T) {
	root := t.TempDir()
	const path = "features/pomodoro/domain.go"
	mine := "package pomodoro\n\nfunc Timer() { /* mine */ }\n"
	if err := os.MkdirAll(filepath.Join(root, "features/pomodoro"), 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(root, path), []byte(mine), filePerm); err != nil {
		t.Fatal(err)
	}

	rec := Receipt{ID: "pomodoro", Files: map[string]string{path: Hash([]byte("original\n"))}}
	plan := Plan(rec, map[string]string{path: "upstream-new\n"}, func(p string) (string, bool) {
		return mine, p == path
	})
	if !plan.Blocked() {
		t.Fatal("fixture must produce a conflict")
	}
	if _, err := Apply(root, plan); err == nil {
		t.Fatal("a blocked plan must not be applied")
	}
	if got := readOrEmpty(t, root, path); got != mine {
		t.Fatalf("nothing may be written on a refused plan, file is %q", got)
	}
}

// TestReceiptRoundTripsThroughYAML asserts the receipt survives persistence, which
// is the only reason it exists: a hash table that cannot be read back answers
// nothing.
func TestReceiptRoundTripsThroughYAML(t *testing.T) {
	root := t.TempDir()
	orig := NewReceipt("pomodoro",
		Upstream{Repo: "github.com/x/y", Ref: "v1.2.0", Path: "features/pomodoro"},
		Declaration{
			Routes: []Route{{Method: "GET", Pattern: "/pomodoro"}},
			Jobs:   []string{"ext:pomodoro:tick"},
			Tables: []string{"ext_pomodoro_session"},
			Slots:  []string{"dashboard.card"},
		},
		map[string]string{"features/pomodoro/page.go": pageSrc})

	if err := SaveReceipt(root, orig); err != nil {
		t.Fatal(err)
	}
	back, err := LoadReceipt(root, "pomodoro")
	if err != nil {
		t.Fatal(err)
	}
	if back.ID != orig.ID || back.Upstream != orig.Upstream {
		t.Fatalf("receipt lost identity: %+v", back)
	}
	if back.Files["features/pomodoro/page.go"] != orig.Files["features/pomodoro/page.go"] {
		t.Fatal("receipt lost a file hash — the whole update story depends on this")
	}
	if len(back.Declaration.Routes) != 1 || back.Declaration.Routes[0].Pattern != "/pomodoro" {
		t.Fatalf("receipt lost the declaration: %+v", back.Declaration)
	}
	if len(back.Declaration.Tables) != 1 || len(back.Declaration.Jobs) != 1 {
		t.Fatalf("receipt lost schema or jobs: %+v", back.Declaration)
	}
}

// TestSaveReceiptRefusesToHijackAnotherRecipe asserts one receipt cannot silently
// replace another's, which is how a rename splits ownership of the same files.
func TestSaveReceiptRefusesToHijackAnotherRecipe(t *testing.T) {
	root := t.TempDir()
	first := Receipt{ID: "pomodoro", Files: map[string]string{"a.go": Hash([]byte("a"))}}
	if err := SaveReceipt(root, first); err != nil {
		t.Fatal(err)
	}
	// Same file path, different id in the body: a corrupt or hand-edited receipt.
	hijack := Receipt{ID: "timer", Files: map[string]string{"a.go": Hash([]byte("a"))}}
	if err := SaveReceipt(root, hijack); err != nil {
		t.Fatal(err)
	}
	// The pomodoro receipt must still be pomodoro.
	back, err := LoadReceipt(root, "pomodoro")
	if err != nil {
		t.Fatal(err)
	}
	if back.ID != "pomodoro" {
		t.Fatalf("receipt was hijacked: %q", back.ID)
	}
}

// TestListReceiptsOnEmptyTreeIsEmpty asserts absence is not an error: a project
// with no recipes is the normal state of a fresh clone.
func TestListReceiptsOnEmptyTreeIsEmpty(t *testing.T) {
	ids, err := ListReceipts(t.TempDir())
	if err != nil {
		t.Fatalf("an empty tree must not error: %v", err)
	}
	if len(ids) != 0 {
		t.Fatalf("expected no recipes, got %v", ids)
	}
}

// TestRemoveLeavesNoReceiptHint asserts Remove does not touch the receipt itself:
// deleting the receipt is a separate, explicit step, because a receipt without
// files is how an operator recovers data ownership after a mistaken uninstall.
func TestRemoveDoesNotDeleteTheReceipt(t *testing.T) {
	root := t.TempDir()
	up := map[string]string{"features/pomodoro/page.go": pageSrc}
	plan := Plan(Receipt{Files: map[string]string{}}, up, func(string) (string, bool) { return "", false })
	if _, err := Apply(root, plan); err != nil {
		t.Fatal(err)
	}
	rec := NewReceipt("pomodoro", Upstream{}, Declaration{}, up)
	if err := SaveReceipt(root, rec); err != nil {
		t.Fatal(err)
	}
	if _, err := Remove(root, rec); err != nil {
		t.Fatal(err)
	}
	if !strings.Contains(readOrEmpty(t, root, ReceiptPath("pomodoro")), "pomodoro") {
		t.Fatal("uninstall must leave the receipt so the data still has an owner")
	}
}
