// SCOPE:core — Observability core: tracing stubs (OTel-ready)
package observability

import "context"

// StartSpan is the OTel-ready span stub. name is intentionally unused until
// the OTel wiring lands — kept in the signature so call sites do not churn.
func StartSpan(ctx context.Context, _ string) (context.Context, func()) {
	return ctx, func() {}
}
