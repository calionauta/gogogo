package queue

import (
	"fmt"
	"os"
	"testing"

	"github.com/calionauta/gogogo/internal/tursotest"
)

// TestMain warms tursogo's shared native-library cache before any test runs.
//
// The extraction is not atomic (os.Create, copy, then compare), so two test
// binaries starting together on a cold cache corrupt it and both fail with a
// hash mismatch that reads like a flaky DSN test. `go test ./...` runs
// internal/queue and features/credits/handlers concurrently and both import
// tursogo, so this is exactly that case. See internal/tursotest for the
// measurements and the cross-process lock that serializes them.
func TestMain(m *testing.M) {
	if err := tursotest.Warm(); err != nil {
		fmt.Fprintf(os.Stderr, "queue: warming the turso library cache failed: %v\n\n"+
			"a hash mismatch on a file that already exists means a previous\n"+
			"extraction was interrupted; delete %s and re-run\n", err, tursotest.CacheDir())
		os.Exit(1)
	}
	os.Exit(m.Run())
}
