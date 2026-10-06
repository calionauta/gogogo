// SCOPE:layer=infra,removal=plugin — installer engine: read-only check gate
package installer

import (
	"strings"
	"testing"
)

// TestCheckTreeRejectsNonTemplateTree covers a misleading failure mode.
//
// Run against an unrelated Go project, checkTree produced one CHECK-FAIL per
// missing dir/marker — 68 lines on a bare `go.mod` — which reads as "this
// project is broken" when the truth is "this is not a gogogo checkout". Both
// exit 1, but only one is actionable, and an LLM calling check_tree on the
// wrong repo got the confusing one.
//
// The positive half (the real template tree still checks clean) is already
// covered by TestCheckTreeAgainstRepoRoot in apply_test.go, so it is not
// repeated here.
func TestCheckTreeRejectsNonTemplateTree(t *testing.T) {
	dir := t.TempDir()
	writeFile(t, dir+"/go.mod", "module example.com/ng\n\ngo 1.27\n")
	writeFile(t, dir+"/cmd/svc/main.go", "package main\n\nfunc main() {}\n")

	var b strings.Builder
	failed := printCheck(&b, checkTree(dir), dir)
	out := b.String()

	if failed == 0 {
		t.Fatal("a non-template tree must not report success")
	}
	if !strings.Contains(out, "not a gogogo checkout") {
		t.Errorf("output must name the real problem:\n%s", out)
	}
	// The per-unit noise is the bug being fixed: a single actionable line.
	if n := strings.Count(out, "\n"); n > 3 {
		t.Errorf("expected one actionable line, got %d lines:\n%s", n, out)
	}
	if strings.Contains(out, "missing dir") || strings.Contains(out, "missing marker") {
		t.Errorf("per-unit inventory is meaningless off-template:\n%s", out)
	}
}

// TestLooksLikeTemplate guards the probe directly, including the case that
// matters: a Go project with a cmd/ tree but no gogogo entry points is NOT
// the template, and must not be mistaken for a trimmed one.
func TestLooksLikeTemplate(t *testing.T) {
	t.Run("not a template", func(t *testing.T) {
		dir := t.TempDir()
		writeFile(t, dir+"/go.mod", "module example.com/ng\n\ngo 1.27\n")
		writeFile(t, dir+"/cmd/svc/main.go", "package main\n\nfunc main() {}\n")
		if looksLikeTemplate(dir) {
			t.Error("a plain Go project must not look like the template")
		}
	})

	t.Run("template entry points present", func(t *testing.T) {
		dir := t.TempDir()
		writeFile(t, dir+"/cmd/web/main.go", "package main\n")
		if !looksLikeTemplate(dir) {
			t.Error("cmd/web/main.go is the template marker")
		}
	})

	t.Run("empty dir", func(t *testing.T) {
		if looksLikeTemplate(t.TempDir()) {
			t.Error("an empty directory must not look like the template")
		}
	})
}
