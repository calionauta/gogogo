package main

import (
	"os"
	"path/filepath"
	"strings"
	"testing"
)

// TestTierRE pins the label matcher against the shapes that must NOT match: a
// two-digit number is not a tier, and a word that merely starts with T and a
// digit is not one either.
func TestTierRE(t *testing.T) {
	for _, tc := range []struct {
		text string
		want []string
	}{
		{"| **T1** format + build |", []string{"T1"}},
		{"T4 fast gate", []string{"T4"}},
		{"T5 verify-local", []string{"T5"}},
		{"T1 format → T2 lint → T3 tests", []string{"T1", "T2", "T3"}},
		{"T6 full local gate → T7 signoff", []string{"T6", "T7"}},
		{"no tier here at all", nil},
		{"TLS1 is a protocol", nil},
		{"T10 is not a tier", nil},
		{"t1 lowercase is not a label", nil},
	} {
		var got []string
		for _, m := range tierRE.FindAllStringSubmatch(tc.text, -1) {
			label := "T" + m[1]
			if m[2] != "" {
				label += "." + m[2]
			}
			got = append(got, label)
		}
		if strings.Join(got, ",") != strings.Join(tc.want, ",") {
			t.Errorf("tierRE on %q = %v, want %v", tc.text, got, tc.want)
		}
	}
}

// TestTierListsAgree is the rule's real assertion: the two pages that describe
// the ladder name the same stops.
func TestTierListsAgree(t *testing.T) {
	root := repoRootForTest(t)
	if problems := checkTierLists(root); len(problems) > 0 {
		t.Fatalf("tier lists drifted: %v", problems)
	}
}

// TestTierListsDetectsDrift is the red-proof: with a page that names T5 and
// another that does not, the rule must report the gap. Without this, a passing
// TestTierListsAgree proves only that the matcher found nothing — which is
// exactly the blind-assertion failure the template's own test doctrine warns
// about.
func TestTierListsDetectsDrift(t *testing.T) {
	dir := t.TempDir()
	write := func(rel, body string) {
		full := filepath.Join(dir, rel)
		if err := os.MkdirAll(filepath.Dir(full), 0o755); err != nil {
			t.Fatal(err)
		}
		if err := os.WriteFile(full, []byte(body), 0o600); err != nil {
			t.Fatal(err)
		}
	}
	// Mirrors tierSources: AGENTS.md names seven stops, the table six.
	write("AGENTS.md", "T1 T2 T3 T4 T5 T6 T7\n")
	write("docs/local-ci.md", "T1 T2 T3 T4 T6 T7\n")

	problems := checkTierLists(dir)
	if len(problems) == 0 {
		t.Fatal("drift between the two tier lists went unreported")
	}
	if !strings.Contains(problems[0], "T5") {
		t.Errorf("the report must name the missing stop, got: %v", problems[0])
	}
}

// TestTierListsAgreeOnEqualSets is the other half: an identical pair of lists
// must pass, so the rule cannot be satisfied by simply always complaining.
func TestTierListsAgreeOnEqualSets(t *testing.T) {
	dir := t.TempDir()
	for _, rel := range tierSources {
		full := filepath.Join(dir, rel)
		if err := os.MkdirAll(filepath.Dir(full), 0o755); err != nil {
			t.Fatal(err)
		}
		if err := os.WriteFile(full, []byte("T1 T2 T3 T4 T5 T6 T7\n"), 0o600); err != nil {
			t.Fatal(err)
		}
	}
	if problems := checkTierLists(dir); len(problems) > 0 {
		t.Fatalf("equal lists reported as drift: %v", problems)
	}
}

// repoRootForTest walks up from the test's working directory to the module.
func repoRootForTest(t *testing.T) string {
	t.Helper()
	dir, err := os.Getwd()
	if err != nil {
		t.Fatal(err)
	}
	for {
		if _, err := os.Stat(filepath.Join(dir, "go.mod")); err == nil {
			return dir
		}
		parent := filepath.Dir(dir)
		if parent == dir {
			t.Fatal("go.mod not found above the test directory")
		}
		dir = parent
	}
}
