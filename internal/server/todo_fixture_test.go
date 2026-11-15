// The reference slice is the suite's fixture for "a real route owner".
//
// That is a reasonable default in the TEMPLATE and a broken assumption in a
// product that dropped it: the tests below assert behavior that a mounted unit
// produces, so with no unit mounted they are asserting nothing. `requireTodo`
// makes the dependency explicit and skips — rather than failing — when the tree
// does not carry the slice. The core behavior each file also covers runs
// regardless.
package server

import (
	"os"
	"path/filepath"
	"testing"
)

// requireTodo skips the test unless this checkout ships features/todo.
func requireTodo(t *testing.T) {
	t.Helper()
	if _, err := os.Stat(filepath.Join("..", "..", "features", "todo")); err != nil {
		t.Skip("features/todo is not installed in this tree: this test asserts behavior of a mounted unit")
	}
}

// hasTodoSlice reports whether this checkout ships the reference slice.
func hasTodoSlice() bool {
	_, err := os.Stat(filepath.Join("..", "..", "features", "todo"))
	return err == nil
}
