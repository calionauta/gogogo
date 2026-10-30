// SCOPE:plugin — installer engine: read-only check gate
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

// TestCheckAnchorReportsDeadAnchors is the red-proof for the half of the
// manifest no gate used to read.
//
// A trim does not need the insertion anchor, so an anchor that no longer
// exists in the target file stays invisible until someone runs `gogogo add`,
// which then fails with "cannot locate insertion anchor (evolved file?)".
// Both real anchors had rotted exactly that way — a `defer shutdown()` and a
// `// Phase 2: CRDTStore` comment that died with the framework — while
// `--check`, CI and pre-commit all stayed green, because every one of them
// asserted only startMarker.
//
// checkAnchor and checkAnchors are driven directly with a synthetic reader,
// so the cases never touch the real checkout.
func TestCheckAnchorReportsDeadAnchors(t *testing.T) {
	live := "package main\n\nfunc main() {\n\tdefer stopLifecycle()\n}\n"
	read := func(p string) (string, bool) {
		switch p {
		case "cmd/web/main.go":
			return live, true
		case "cmd/desktop/main.go":
			// The desktop anchor is a different line; each entry point must be
			// probed against its OWN content.
			return "package main\n\nfunc main() {\n\tviewURL := webviewURL(cfg.Host, port)\n}\n", true
		default:
			return "", false
		}
	}

	t.Run("rule anchor present passes", func(t *testing.T) {
		var uc unitCheck
		checkAnchor(&uc, "cmd/web/main.go", live, "\tdefer stopLifecycle()")
		if len(uc.Problems) != 0 {
			t.Errorf("a live anchor must not be reported: %v", uc.Problems)
		}
	})

	t.Run("rule anchor absent is reported", func(t *testing.T) {
		var uc unitCheck
		checkAnchor(&uc, "cmd/web/main.go", live, "\tdefer DEAD()")
		if len(uc.Problems) != 1 || !strings.Contains(uc.Problems[0], "dead add anchor") {
			t.Errorf("a dead anchor must be reported, got %v", uc.Problems)
		}
	})

	t.Run("empty anchor is the path default, not a problem", func(t *testing.T) {
		var uc unitCheck
		checkAnchor(&uc, "cmd/web/main.go", live, "")
		if len(uc.Problems) != 0 {
			t.Errorf("empty means 'use the default', validated elsewhere: %v", uc.Problems)
		}
	})

	t.Run("default anchors are checked on both entry points", func(t *testing.T) {
		if got := checkAnchors(read); len(got) != 0 {
			t.Errorf("live defaults must pass, got %v", got)
		}
		dead := func(string) (string, bool) { return "package main\n", true }
		got := checkAnchors(dead)
		// cmd/web/main.go uses anchorAfter(mainCallsAnchor) and cmd/desktop
		// anchorBefore(desktopBlockAnchor); both must be probed.
		if len(got) != 2 {
			t.Errorf("expected both entry points probed, got %v", got)
		}
	})
}
