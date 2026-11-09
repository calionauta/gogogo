// Delivery transport wiring is owned by the inbox unit (Register). This
// file only adapts the published bus to the browser's leader session
// stream, so the rail badge hears about a delivery without a second
// connection per page. Without the inbox there is no feed, and without
// the feed the stream stays session-only.
package server

import (
	"github.com/calionauta/gogogo/features/notifications"
	"github.com/calionauta/gogogo/internal/auth"
	dbgen "github.com/calionauta/gogogo/internal/db/sqlcgen"
	"github.com/calionauta/gogogo/internal/recipe/register"
)

// mountInbox wires the inbox bus into the leader session stream.
func mountInbox(a *auth.Auth, q *dbgen.Queries, ref *register.InboxRef) {
	if ref == nil || ref.Bus == nil {
		return
	}
	bus, ok := ref.Bus.(*notifications.Bus)
	if !ok {
		return
	}
	a.SetSessionStreamExtras(inboxShellFeed{bus: bus, q: q})
}
