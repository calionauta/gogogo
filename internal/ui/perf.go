// SCOPE:core — Request-scoped "runtime dashboard is enabled" flag for the shell.
//
// The menu entry is offered only when BOTH this and Operator(ctx) are true: the
// composition root seeds the build/runtime flag here, and the operator
// middleware decides the identity. Keeping the two separate means a disabled
// dashboard never renders a link, and an enabled one is never offered to a
// non-operator.
package ui

import "context"

type perfKey struct{}

// WithPerf seeds "the runtime dashboard is mounted" for one request.
func WithPerf(ctx context.Context, ok bool) context.Context {
	return context.WithValue(ctx, perfKey{}, ok)
}

// Perf reports whether the runtime dashboard is mounted for this request.
func Perf(ctx context.Context) bool {
	ok, _ := ctx.Value(perfKey{}).(bool)
	return ok
}
