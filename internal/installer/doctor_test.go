package installer

import (
	"testing"
)

// doctor_test covers the checks whose verdict is easy to get silently wrong.
// The rest of runAllChecks probes the host (go version, xcode, docker) and is
// exercised by `make doctor`, not here — asserting on it in a unit test would
// make the suite depend on whatever machine it runs on.

// A tool pinned in go.mod lives in the module cache and is invoked as
// `go tool <name>`; it is NOT on PATH. Probing PATH would report a false
// failure on a healthy checkout, which is the failure mode this pins down.
func TestGoToolCheckReportsMissingPinAsWarnNotFail(t *testing.T) {
	c := checkGoToolWarn("go.tool.gremlins", "definitely-not-a-pinned-tool", "go get -tool example.com/x")
	if c.Status != checkWarn {
		t.Fatalf("a missing opt-in tool must be warn (never fail, it would block dev): got %q", c.Status)
	}
	if c.Fix == "" {
		t.Fatal("a warn without a fix tells the user nothing actionable")
	}
}

// The check must be reachable by its documented id, because `doctor --only` and
// the JSON contract both address checks that way. Without this, a refactor could
// drop the check from runAllChecks and every other test would still pass.
func TestGremlinsCheckIsRegistered(t *testing.T) {
	rep, _ := runAllChecks("go.tool.gremlins")
	if len(rep.Checks) != 1 {
		t.Fatalf("expected exactly the gremlins check, got %d: %v", len(rep.Checks), rep.Checks)
	}
	if got := rep.Checks[0].ID; got != "go.tool.gremlins" {
		t.Fatalf("check id drifted: %q", got)
	}
	// Skipped/warn/pass all acceptable: the point is that it ran and reported a
	// status, not that this machine has gremlins.
	if rep.Checks[0].Status == "" {
		t.Fatal("check produced no status")
	}
}
