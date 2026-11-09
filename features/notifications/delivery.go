// Notifications delivery contract (shared by the handler and its callers).
package notifications

import (
	"context"

	"github.com/calionauta/gogogo/internal/delivery"
)

// Delivery is one user-facing message. The in-app row is always written here;
// Channels asks the transports to carry the same message too. Channels and
// Recipients are keyed by the same channel so a second channel (sms, chat,
// push) is a value in the list, not a new field on every caller.
type Delivery struct {
	// Owner is the users.id the row belongs to. It comes from the session or
	// from the run input, never from a client-supplied id.
	Owner string
	// TransactionID is the dedupe key: one delivery per owner, transaction and
	// channel. A retried step reuses it and becomes a no-op.
	TransactionID string
	// Workflow names the producer ("reminder", "onboarding"), so a reader can
	// tell two messages apart without parsing the body.
	Workflow string
	Subject  string
	Body     string
	// Channels lists the external channels to attempt. ChannelInApp is ignored:
	// the inbox row is written unconditionally above the loop.
	Channels []delivery.Channel
	// Recipients maps channel -> address (email address, phone, chat id). An
	// empty address records skipped/no_address instead of sending nowhere.
	Recipients map[delivery.Channel]string
}

// RunStarter is the slice of the workflow client the feature needs. It lives
// here (not in handlers) so the composition root can inject the DagNats
// client without the feature importing the engine.
type RunStarter interface {
	StartRun(ctx context.Context, workflow string, input any) (string, error)
}

// Deliverer is what engine workers need: write one delivery row. The run
// starter setter keeps the feature free of an internal/dagnats import.
// Wired reports whether scheduling is possible (the engine started in
// this binary), the truth the operator link and the reminder form share.
type Deliverer interface {
	Deliver(ctx context.Context, d Delivery) error
	SetRunStarter(r RunStarter)
	Wired() bool
}
