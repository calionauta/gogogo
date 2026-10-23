package queue

import (
	"testing"

	"go.uber.org/goleak"
)

// TestMain verifies the whole package for leaked goroutines after every test
// has finished, instead of asserting per-test.
//
// Per-test goleak.VerifyNone is incompatible with t.Parallel by construction
// (upstream docs): the check is process-global and cannot tell a goroutine
// leaked by this test apart from one still running in a sibling parallel
// test, so a slow sibling reads as a leak here. Package-level verification
// runs when no test goroutine is left, which removes that class of flake
// while keeping the regression signal — a real leak still fails the package,
// with the creation stack pointing at the owner.
func TestMain(m *testing.M) {
	goleak.VerifyTestMain(m,
		goleak.IgnoreTopFunction("database/sql.(*DB).connectionOpener"),
	)
}
