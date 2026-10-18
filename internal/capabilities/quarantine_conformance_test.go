// SCOPE:core — conformance guard for the quarantine registry.
//
// The quarantine mechanism (internal/hostgate/quarantine.go) lets a known-flaky
// test stop gating the build while keeping its reason, its issue link and a
// deadline on record. That mechanism is only honest if a bad entry is a RED
// BUILD rather than a surprise months later, so this guard asks the registry the
// same question the mechanical gates ask of everything else: is the state
// actually valid right now?
//
// It is the only thing preventing the failure mode that makes quarantines
// untrustworthy — an entry that expires and nobody notices, so the test stays
// skipped forever and the coverage silently evaporates. `ValidateQuarantines`
// fails on an expired entry by design, and this test is what makes CI enforce it.
package capabilities

import (
	"testing"
	"time"

	"github.com/calionauta/gogogo/internal/hostgate"
)

func TestQuarantineRegistryIsValid(t *testing.T) {
	t.Parallel()
	errs := hostgate.ValidateQuarantines(time.Now())
	if len(errs) == 0 {
		// Rare in a good way: an EMPTY registry means nothing is quarantined,
		// which is the state this repo should spend most of its life in.
		if names := hostgate.Quarantined(); len(names) != 0 {
			t.Fatalf("registry has %d entries but validated clean: %v", len(names), names)
		}
		t.Log("no quarantined tests — nothing is skipped on flake grounds")
		return
	}
	for _, err := range errs {
		t.Errorf("quarantine registry is invalid: %v", err)
	}
	t.Errorf("fix each entry above (reason + issue + a FUTURE date), or fix the test "+
		"and delete the entry — see docs/ci-triage.md. Currently registered: %v",
		hostgate.Quarantined())
}
