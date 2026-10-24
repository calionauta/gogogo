package server

import (
	"context"
)

// Model stubs for the composition tests (TestBuildChiMux*): deterministic
// stand-ins for the Chat model leg so routing asserts never touch a model.
//
// Lives in its own file (not chi_test.go) so trimming the genui bundle
// deletes it via the capability Files list — the same shape as the DagNats
// engine files owned outside their own directory.

// stubResponder answers model calls with fixed output.
type stubResponder struct{ raw string }

func (s stubResponder) Respond(context.Context, string) (string, error) { return s.raw, nil }
