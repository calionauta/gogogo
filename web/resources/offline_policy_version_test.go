// SCOPE:core — the offline policy's self-fingerprint.
//
// WHY THIS EXISTS. offline-policy.js is imported by the Service Worker at
// install time and loaded by every notes/whiteboard page. A worker KEEPS the
// copy it imported until it reinstalls, so after a deploy the page can run a
// newer policy than the worker — the state the offline banner warns about
// ("offline sync is out of date — reload"). Detecting it needs a value both
// sides can compare, and a hand-maintained constant goes stale the first time
// someone edits the file in a hurry.
//
// The version is therefore derived from the file itself: the first 6 hex of the
// sha256 of its bytes with the version VALUE blanked (so the hash does not
// depend on the value it produces). This test recomputes it, so editing the
// policy without updating the constant is a red build — and the failure message
// prints the value to paste.
package resources

import (
	"crypto/sha256"
	"encoding/hex"
	"io/fs"
	"regexp"
	"testing"
)

// policyVersionRe matches the assignment; blanking its value (not the whole
// line) keeps the digest independent of indentation and of the value itself.
var policyVersionRe = regexp.MustCompile(`POLICY_VERSION = "[^"]*";`)

// policyVersionValueRe captures the value the file currently carries.
var policyVersionValueRe = regexp.MustCompile(`POLICY_VERSION = "([^"]*)";`)

func TestOfflinePolicyVersionMatchesItsDigest(t *testing.T) {
	t.Parallel()
	raw, err := fs.ReadFile(StaticFS(), "offline-policy.js")
	if err != nil {
		t.Fatalf("read embedded offline-policy.js: %v", err)
	}

	norm := policyVersionRe.ReplaceAll(raw, []byte(`POLICY_VERSION = "";`))
	sum := sha256.Sum256(norm)
	want := hex.EncodeToString(sum[:])[:6]

	m := policyVersionValueRe.FindSubmatch(raw)
	if m == nil {
		t.Fatal("offline-policy.js has no POLICY_VERSION assignment; the banner's staleness check cannot work without it")
	}
	if got := string(m[1]); got != want {
		t.Errorf("offline-policy.js POLICY_VERSION = %q, want %q (sha256 of the file with the value blanked).\n"+
			"Set it to %q — the page and the worker compare this value to detect a worker from an older deploy.",
			got, want, want)
	}
}
