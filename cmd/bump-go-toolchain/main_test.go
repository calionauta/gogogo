package main

import (
	"context"
	"os"
	"path/filepath"
	"strings"
	"testing"
)

// TestCheckTargetRefusesWhatItMust: a bump that leaves the minor line, that
// goes backwards, or that names a version that is not a version.
func TestCheckTargetRefusesWhatItMust(t *testing.T) {
	t.Parallel()

	cases := []struct {
		name    string
		current string
		line    string
		target  string
		want    string
	}{
		{"minor bump", "1.27.3", "1.27", "1.28.0", "MINOR"},
		{"downgrade", "1.27.3", "1.27", "1.27.2", "backwards"},
		{"same version", "1.27.3", "1.27", "1.27.3", "not newer"},
		{"not a version", "1.27.3", "1.27", "1.27", "not a full version"},
		{"different major", "1.27.3", "1.27", "2.27.4", "MINOR"},
	}
	for _, c := range cases {
		t.Run(c.name, func(t *testing.T) {
			t.Parallel()
			err := checkTarget(c.current, c.line, c.target)
			if err == nil {
				t.Fatalf("checkTarget(%s, %s) accepted %s", c.current, c.line, c.target)
			}
			if !strings.Contains(err.Error(), c.want) {
				t.Errorf("error %q does not explain %q", err, c.want)
			}
		})
	}

	if err := checkTarget("1.27.3", "1.27", "1.27.4"); err != nil {
		t.Errorf("checkTarget refused a legitimate patch bump: %v", err)
	}
}

// TestRunIsANoOpWhenAlreadyAtTheTarget: asking for the version the tree
// already has must write nothing.
func TestRunIsANoOpWhenAlreadyAtTheTarget(t *testing.T) {
	t.Parallel()
	root := writeFixture(t)

	var out, errOut strings.Builder
	err := run(context.Background(),
		[]string{"--root", root, "--to", "1.27.3"},
		&out, &errOut)
	if err != nil {
		t.Fatalf("run: %v", err)
	}
	if !strings.Contains(out.String(), "already at 1.27.3") {
		t.Errorf("run did not report a no-op:\n%s", out.String())
	}
	if body := read(t, root, "go.mod"); !strings.Contains(body, "go 1.27.3") {
		t.Errorf("a no-op rewrote go.mod:\n%s", body)
	}
}

// TestExplicitTargetMustBePublished: an explicit --to is confirmed against
// go.dev before the first byte is written. Without this, a typo lands a pin
// nobody published, every CI job fails at "download the toolchain", and the
// commit that caused it looks innocent.
func TestExplicitTargetMustBePublished(t *testing.T) {
	t.Parallel()
	root := writeFixture(t)

	var out, errOut strings.Builder
	err := runWith(context.Background(),
		[]string{"--root", root, "--to", "1.27.5"},
		&out, &errOut, fakeOnline(t, "", "1.27.4"))
	if err == nil {
		t.Fatal("run accepted an explicit target that go.dev does not publish")
	}
	if !strings.Contains(err.Error(), "not a published Go release") {
		t.Errorf("error %q does not explain that the version does not exist", err)
	}
	for _, f := range surface {
		if body := read(t, root, f.Path); !containsVersionToken(body, "1.27.3") {
			t.Errorf("%s was rewritten although the target was refused:\n%s", f.Path, body)
		}
	}
}

// TestExplicitTargetIsWrittenWhenPublished is the other half: the check must
// not be a blanket refusal.
func TestExplicitTargetIsWrittenWhenPublished(t *testing.T) {
	t.Parallel()
	root := writeFixture(t)

	var out, errOut strings.Builder
	if err := runWith(context.Background(),
		[]string{"--root", root, "--to", "1.27.4"},
		&out, &errOut, fakeOnline(t, "", "1.27.4")); err != nil {
		t.Fatalf("run: %v (stderr: %s)", err, errOut.String())
	}
	if body := read(t, root, "go.mod"); !strings.Contains(body, "go 1.27.4") {
		t.Errorf("go.mod did not move to the published target:\n%s", body)
	}
}

// TestDetectReadsTheFeed drives -detect through a server instead of go.dev:
// this is the branch the weekly workflow runs unattended, so it is the one
// most worth an end-to-end test.
func TestDetectReadsTheFeed(t *testing.T) {
	t.Parallel()
	root := writeFixture(t)

	var out, errOut strings.Builder
	feed := `[{"version":"go1.27.4","stable":true},{"version":"go1.28.0","stable":true}]`
	if err := runWith(context.Background(),
		[]string{"--root", root, "-detect"},
		&out, &errOut, fakeOnline(t, feed)); err != nil {
		t.Fatalf("run: %v (stderr: %s)", err, errOut.String())
	}
	if got := strings.TrimSpace(out.String()); got != "1.27.4" {
		t.Errorf("-detect printed %q, want 1.27.4 (newest patch of the tree's line, not the newer minor)", got)
	}
	for _, f := range surface {
		if body := read(t, root, f.Path); !containsVersionToken(body, "1.27.3") {
			t.Errorf("-detect rewrote %s:\n%s", f.Path, body)
		}
	}
}

// TestDryRunWritesNothing: --dry-run must report and change NOTHING.
//
// The first implementation computed the rewrite and wrote it before looking
// at the flag, so a dry run on a real tree bumped it for real. The flag was
// decorative. This test is why it is not decorative any more.
func TestDryRunWritesNothing(t *testing.T) {
	t.Parallel()
	root := writeFixture(t)

	var out, errOut strings.Builder
	err := runWith(context.Background(),
		[]string{"--root", root, "--to", "1.27.4", "--dry-run"},
		&out, &errOut, fakeOnline(t, "", "1.27.4"))
	if err != nil {
		t.Fatalf("run: %v", err)
	}
	if !strings.Contains(out.String(), "dry run") {
		t.Errorf("run did not report a dry run:\n%s", out.String())
	}
	for _, f := range surface {
		body := read(t, root, f.Path)
		if containsVersionToken(body, "1.27.4") {
			t.Errorf("%s was rewritten by a dry run:\n%s", f.Path, body)
		}
		if !containsVersionToken(body, "1.27.3") {
			t.Errorf("%s lost Go 1.27.3 during a dry run:\n%s", f.Path, body)
		}
	}
}

// TestRunRefusesAMinorBumpThroughTheCLI: the policy is enforced at the
// boundary a human or a workflow actually uses.
func TestRunRefusesAMinorBumpThroughTheCLI(t *testing.T) {
	t.Parallel()
	root := writeFixture(t)

	var out, errOut strings.Builder
	err := runWith(context.Background(),
		[]string{"--root", root, "--to", "1.28.0"},
		&out, &errOut, fakeOnline(t, "", "1.28.0"))
	if err == nil {
		t.Fatal("run accepted a minor bump — that decision is a human's")
	}
	if !strings.Contains(err.Error(), "MINOR") {
		t.Errorf("error %q does not explain the policy", err)
	}
	if body := read(t, root, "go.mod"); !strings.Contains(body, "go 1.27.3") {
		t.Errorf("a refused bump still rewrote go.mod:\n%s", body)
	}
}

// TestSurfaceMatchesTheRepository: every file the tool rewrites must exist
// in the tree, and the tree must still be at the version the tool reads.
// A typo in the surface is a bump that silently does nothing.
func TestSurfaceMatchesTheRepository(t *testing.T) {
	t.Parallel()
	root, err := findRepoRoot(".")
	if err != nil {
		t.Fatalf("findRepoRoot: %v", err)
	}

	for _, f := range surface {
		if _, statErr := os.Stat(filepath.Join(root, f.Path)); statErr != nil {
			t.Errorf("the surface names %s, which is not in the tree: %v", f.Path, statErr)
		}
		if strings.TrimSpace(f.Why) == "" {
			t.Errorf("the surface names %s with no reason — every entry has to justify why "+
				"it carries the pin, or the next person cannot tell when it stops", f.Path)
		}
	}

	current, line, err := currentVersion(root)
	if err != nil {
		t.Fatalf("currentVersion: %v", err)
	}
	if _, _, _, ok := parseVersion(current); !ok {
		t.Errorf("the tree declares %q, which is not a full version — the bump tool "+
			"only moves full patch versions", current)
	}
	if line == "" {
		t.Error("the tree's release line could not be read")
	}
}
