// The analytics beacon is page-scoped, like the operator and perf flags.
//
// The flag is seeded per navigation by the server (it knows whether a provider
// is configured), so the shell renders the script only where it would actually
// send something and no template reads config.
package ui

import "context"

// analyticsKey marks a request whose shell should load the beacon script.
type analyticsKey struct{}

// WithAnalytics seeds the flag on a page request.
func WithAnalytics(ctx context.Context, on bool) context.Context {
	return context.WithValue(ctx, analyticsKey{}, on)
}

// Analytics reports whether the shell should render the beacon script.
func Analytics(ctx context.Context) bool {
	on, _ := ctx.Value(analyticsKey{}).(bool)
	return on
}
