// SCOPE:layer=feature,removal=feature — Todo HTTP failure helper (log + plain response)
package handlers

import (
	"log/slog"

	"github.com/pocketbase/pocketbase/core"
)

// fail logs the internal error with context and returns a plain-text
// 500 with a safe public message. One helper so handlers don't repeat
// the slog.Error + c.String pair and can't drift (one site logging
// with filter, another without) or leak internals into the body
// ("enqueue failed: "+err.Error() shipped the driver text to the client).
//
// Always 500 by design: this is the internal-error exit only. Other
// statuses (400/404/503) keep their explicit c.String — they are
// client states, not failures, and collapsing them here would hide intent.
//
// Non-SSE errors stay plain-text by design: record mutations propagate
// to other tabs via PocketBase realtime + fragment re-fetch, the SSE
// hub carries only ephemeral signals. When the failing request already
// owns an SSE stream, callers use emitToast instead.
func fail(c *core.RequestEvent, logMsg string, err error, publicMsg string) error {
	slog.Error(logMsg, "error", err)
	return c.String(statusInternal, publicMsg)
}
