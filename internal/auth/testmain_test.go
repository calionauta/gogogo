// SCOPE:core — Auth test cost policy (cheap argon by default).
package auth

import (
	"os"
	"testing"
)

// TestMain stages cheap argon parameters for the whole package: password
// hashing costs ~250ms per hash by calibration, and this package burns
// dozens per run (the signin throttle test alone needs 30+ to cross its
// budget) — at full cost the package is the ci-local critical path. Cheap
// params exercise identical logic (verify, compare, throttle, seal) in
// ~1ms; real-cost compatibility stays pinned by TestPasswordHashRoundTrip
// and TestPasswordRotation, which re-stage the production entry
// explicitly. A new test inherits cheap unless it opts into real.
func TestMain(m *testing.M) {
	real, ok := argonVersions[1]
	if !ok {
		os.Exit(m.Run())
	}
	realArgonV1 = real
	argonVersions[1] = argonParams{time: 1, memory: 8 * 1024, threads: 1, keyLen: 32}
	code := m.Run()
	argonVersions[1] = real
	os.Exit(code)
}

// realArgonV1 is the production v1 entry captured before TestMain stages
// cheap params, so compat proofs can re-stage exactly what ships. Zero
// until TestMain runs (a test binary always runs it first).
var realArgonV1 argonParams

// withRealArgon re-stages the production v1 parameters for the compat
// proofs that must verify what production writes.
func withRealArgon(t *testing.T) {
	t.Helper()
	if realArgonV1.memory == 0 {
		t.Fatal("real v1 params not captured")
	}
	old, ok := argonVersions[1]
	if !ok {
		t.Fatal("v1 params missing")
	}
	argonVersions[1] = realArgonV1
	t.Cleanup(func() { argonVersions[1] = old })
}
