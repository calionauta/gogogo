// SCOPE:core — Request-scoped inbox count for the rail indicator.
//
// The rail is rendered by every page, but only the notifications feature owns
// the count query. The composition root resolves it once per page request and
// seeds it here, so a page template never imports the delivery store and a
// feature trim cannot leave a second copy of the query behind.
package ui

import "context"

type inboxUnreadKey struct{}

// WithInboxUnread seeds the rail's unread count for one request.
func WithInboxUnread(ctx context.Context, n int) context.Context {
	return context.WithValue(ctx, inboxUnreadKey{}, n)
}

// InboxUnread reads the rail's unread count. A page rendered outside the
// composition root (a component test, a trimmed build) reads 0, which renders
// the same rail without a bubble.
func InboxUnread(ctx context.Context) int {
	n, _ := ctx.Value(inboxUnreadKey{}).(int)
	if n < 0 {
		return 0
	}
	return n
}
