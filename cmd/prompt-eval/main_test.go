package main

import (
	"os"
	"path/filepath"
	"strconv"
	"strings"
	"testing"

	"github.com/calionauta/gogogo/internal/prompts"
)

// writeCandidate lays one prompt file under dir.
func writeCandidate(t *testing.T, dir, id, body string, version int) {
	t.Helper()
	path := filepath.Join(dir, filepath.FromSlash(id+".prompt"))
	if err := os.MkdirAll(filepath.Dir(path), 0o750); err != nil {
		t.Fatal(err)
	}
	text := "---\nname: " + id + "\ndescription: \"candidate\"\nversion: " +
		strconv.Itoa(version) + "\noutput: text\nvariables: []\n---\n" + body + "\n"
	if err := os.WriteFile(path, []byte(text), 0o600); err != nil {
		t.Fatal(err)
	}
}

// basePrompt returns an id the embedded library really has.
func basePrompt(t *testing.T) prompts.Prompt {
	t.Helper()
	all, err := prompts.All()
	if err != nil || len(all) == 0 {
		t.Fatalf("read library: %v", err)
	}
	return all[0]
}

// TestUnchangedBodyIsReportedAsUnchanged is the positive control: a copy of the
// library must not be a diff.
func TestUnchangedBodyIsReportedAsUnchanged(t *testing.T) {
	base := basePrompt(t)
	dir := t.TempDir()
	writeCandidate(t, dir, base.ID, base.Body, base.Version)

	rep, err := evaluate(dir)
	if err != nil {
		t.Fatalf("evaluate: %v", err)
	}
	if rep.Changed != 0 || rep.Invalid != 0 {
		t.Fatalf("a verbatim copy produced a diff: %+v", rep)
	}
}

// TestUnbumpedRewriteIsInvalid is the reason the command exists: a body change
// without a version bump makes the previous evaluation incomparable.
func TestUnbumpedRewriteIsInvalid(t *testing.T) {
	base := basePrompt(t)
	dir := t.TempDir()
	writeCandidate(t, dir, base.ID, base.Body+"\nA new instruction.", base.Version)

	rep, err := evaluate(dir)
	if err != nil {
		t.Fatalf("evaluate: %v", err)
	}
	if rep.Changed != 1 || rep.Invalid != 1 {
		t.Fatalf("unbumped rewrite: changed=%d invalid=%d, want 1 and 1", rep.Changed, rep.Invalid)
	}
	if !strings.Contains(strings.Join(rep.Rows[0].Notes, " "), "not comparable") {
		t.Errorf("the note does not explain the version rule: %v", rep.Rows[0].Notes)
	}
}

// TestBumpedRewriteIsAValidChange: the same edit with a higher version is the
// case the loop is supposed to produce.
func TestBumpedRewriteIsAValidChange(t *testing.T) {
	base := basePrompt(t)
	dir := t.TempDir()
	writeCandidate(t, dir, base.ID, base.Body+"\nA new instruction.", base.Version+1)

	rep, err := evaluate(dir)
	if err != nil {
		t.Fatalf("evaluate: %v", err)
	}
	if rep.Changed != 1 || rep.Invalid != 0 {
		t.Fatalf("bumped rewrite: changed=%d invalid=%d, want 1 and 0 (%v)",
			rep.Changed, rep.Invalid, rep.Rows)
	}
}

// TestInventedIDIsRefused: a file the library has never heard of has no call
// site, so it cannot be evaluated.
func TestInventedIDIsRefused(t *testing.T) {
	dir := t.TempDir()
	writeCandidate(t, dir, "genui/nothing-loads-this", "body", 1)

	rep, err := evaluate(dir)
	if err != nil {
		t.Fatalf("evaluate: %v", err)
	}
	if rep.Added != 1 || rep.Invalid != 1 {
		t.Fatalf("invented id: added=%d invalid=%d, want 1 and 1", rep.Added, rep.Invalid)
	}
}

// TestRemovedPromptsAreNamed keeps the report honest: a tree holding one file
// must not read as "no difference from the library".
func TestRemovedPromptsAreNamed(t *testing.T) {
	base := basePrompt(t)
	dir := t.TempDir()
	writeCandidate(t, dir, base.ID, base.Body, base.Version)

	rep, err := evaluate(dir)
	if err != nil {
		t.Fatalf("evaluate: %v", err)
	}
	if rep.Removed == 0 {
		t.Fatal("a one-file tree reported no removals")
	}
	all, _ := prompts.All()
	if rep.Removed != len(all)-1 {
		t.Errorf("removed = %d, want %d", rep.Removed, len(all)-1)
	}
}

// TestUnparseableCandidateIsInvalid: the tree must load before it can be judged.
func TestUnparseableCandidateIsInvalid(t *testing.T) {
	base := basePrompt(t)
	dir := t.TempDir()
	path := filepath.Join(dir, filepath.FromSlash(base.ID+".prompt"))
	if err := os.MkdirAll(filepath.Dir(path), 0o750); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(path, []byte("not frontmatter at all"), 0o600); err != nil {
		t.Fatal(err)
	}
	rep, err := evaluate(dir)
	if err != nil {
		t.Fatalf("evaluate: %v", err)
	}
	if rep.Invalid != 1 {
		t.Fatalf("invalid = %d, want 1", rep.Invalid)
	}
}
