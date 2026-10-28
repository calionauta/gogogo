// SCOPE:core — Request-scoped operator flag for the shell.
//
// The account menu offers the DagNats console only to an operator, because the
// console answers 403 to everyone else (admin AND an ADMIN_EMAILS match; the
// public demo admin is not an operator). The shell is rendered by every
// feature, so the composition root resolves the answer once per page request
// and seeds it here — a page template never reads the config or re-implements
// the rule, and a trim cannot leave a stale copy behind.
package ui

import "context"

type operatorKey struct{}

// WithOperator seeds "this session may open the operator surfaces" for one
// request.
func WithOperator(ctx context.Context, ok bool) context.Context {
	return context.WithValue(ctx, operatorKey{}, ok)
}

// Operator reports whether the signed-in session is an operator. A page
// rendered outside the composition root (a component test, a trimmed build)
// reads false, which renders the same menu without the console entry.
func Operator(ctx context.Context) bool {
	ok, _ := ctx.Value(operatorKey{}).(bool)
	return ok
}
