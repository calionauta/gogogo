// SCOPE:plugin — installer engine: version ordering tests
package installer

import "testing"

// TestParseSemverRejectsUnorderable pins the boundary that matters: anything
// this project does not ship must report "not comparable" instead of landing
// on an arbitrary side of a release. A guessed order is how "0.53" ends up
// ranked above "0.53.1".
func TestParseSemverRejectsUnorderable(t *testing.T) {
	for _, raw := range []string{"", "  ", "dev", "0.53", "0.53.0.1", "v", "abc", "1.2.x", "main"} {
		if _, ok := parseSemver(raw); ok {
			t.Errorf("parseSemver(%q) must not be comparable", raw)
		}
	}
}

func TestParseSemverAcceptsReleasedShapes(t *testing.T) {
	for _, raw := range []string{"0.53.0", "v0.53.0", "v0.53.0-rc.1", "0.53.0+build.7", "0.53.0-rc.1+sha"} {
		if _, ok := parseSemver(raw); !ok {
			t.Errorf("parseSemver(%q) must parse", raw)
		}
	}
}

// TestCompareSemverPrecedence is the contract both callers lean on: the update
// check ("am I behind?") and the migration range ("which releases are newer?").
func TestCompareSemverPrecedence(t *testing.T) {
	a, _ := parseSemver("0.53.0")
	b, _ := parseSemver("0.54.0")
	if got := compareSemver(a, b); got != -1 {
		t.Errorf("0.53.0 vs 0.54.0 = %d, want -1", got)
	}
	if got := compareSemver(b, a); got != 1 {
		t.Errorf("0.54.0 vs 0.53.0 = %d, want 1", got)
	}
	same, _ := parseSemver("v0.53.0")
	if got := compareSemver(a, same); got != 0 {
		t.Errorf("v-prefixed equal versions = %d, want 0", got)
	}
	// A pre-release is BEHIND its release: "0.54.0-rc.1 installed, 0.54.0
	// available" must not read as current.
	pre, _ := parseSemver("0.54.0-rc.1")
	rel, _ := parseSemver("0.54.0")
	if got := compareSemver(pre, rel); got != -1 {
		t.Errorf("rc vs release = %d, want -1", got)
	}
	pre2, _ := parseSemver("0.54.0-rc.2")
	if got := compareSemver(pre, pre2); got != -1 {
		t.Errorf("rc.1 vs rc.2 = %d, want -1", got)
	}
}

// TestVersionStatusCoversEveryVerdict keeps the four statuses honest, because
// they are a JSON contract an agent branches on.
func TestVersionStatusCoversEveryVerdict(t *testing.T) {
	cases := []struct{ installed, latest, want string }{
		{"0.53.0", "v0.54.0", statusBehind},
		{"0.54.0", "v0.54.0", statusCurrent},
		{"0.55.0", "v0.54.0", statusAhead},
		{"dev", "v0.54.0", statusUnknown},
		{"0.54.0", "not-a-version", statusUnknown},
	}
	for _, c := range cases {
		if got := versionStatus(c.installed, c.latest); got != c.want {
			t.Errorf("versionStatus(%q, %q) = %q, want %q", c.installed, c.latest, got, c.want)
		}
	}
}

// TestShortCommitIsDisplayOnly: the marker stores the full SHA, and truncation
// must never happen in the record itself.
func TestShortCommitIsDisplayOnly(t *testing.T) {
	if got := shortCommit("0123456789abcdef0123456789abcdef01234567"); got != "0123456" {
		t.Errorf("shortCommit = %q, want 0123456", got)
	}
	if got := shortCommit("unknown"); got != "unknown" {
		t.Errorf("shortCommit(unknown) = %q", got)
	}
}
