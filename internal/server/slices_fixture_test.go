// Which server tests need a slice, stated instead of implied.
//
// Most of this package's tests assert core behavior and run in any tree. A few
// describe a slice's COMPOSITION: the retry demo's worker reaching the page's
// stream, the dashboard summing three slices' tables. Those can only run where
// the slices exist, and their subject is the slice, not the seam.
//
// `requireSlices` makes that dependency one visible line with a reason. It is
// deliberately the exception: a test that can assert a core surface must do
// that instead (see extinvoke_test.go and units_mount_test.go, which stopped
// needing a slice at all).
package server

import (
	"os"
	"path/filepath"
	"testing"
)

// requireSlices skips the test unless this checkout ships every named slice.
func requireSlices(t *testing.T, ids ...string) {
	t.Helper()
	for _, id := range ids {
		if _, err := os.Stat(filepath.Join("..", "..", "features", id)); err != nil {
			t.Skipf("features/%s is not installed: this test asserts a slice's composition, not the seam", id)
		}
	}
}
