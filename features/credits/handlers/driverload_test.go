package handlers

import (
	"fmt"
	"os"
	"testing"

	"github.com/calionauta/gogogo/internal/dbload"
)

// TestMain loads the SQLite driver's native library before any test runs, so
// this binary and its siblings cannot corrupt the shared cache by extracting it
// at the same instant. See internal/dbload for the race and the lock.
//
// `go test ./...` runs one binary per package concurrently and several packages
// import the driver, so the sibling in features/credits/handlers is always
// there. Without this, both fail with a hash mismatch that reads like the DSN
// test being flaky.
func TestMain(m *testing.M) {
	if err := dbload.Ensure(); err != nil {
		fmt.Fprintf(os.Stderr, "credits handlers: loading the SQLite driver failed: %v\n", err)
		os.Exit(1)
	}
	os.Exit(m.Run())
}
