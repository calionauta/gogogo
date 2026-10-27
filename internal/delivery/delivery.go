// SCOPE:plugin — delivery transport seam.
//
// The inbox is NOT a transport. `in_app` is written by the notifications
// feature, next to the user row, under the same owner scoping and the same
// deletion path. A provider behind this seam only carries the channels the app
// cannot deliver itself (email today, sms/push later), which is what keeps one
// owner for the inbox and one answer for "was the user told".
//
// One method plus a name, so a provider is small enough to replace in an
// afternoon. The feature depends on this interface, never on a provider
// package; the composition root builds the router.
package delivery

import "context"

// Channel is one delivery channel — the medium the person perceives, not the
// provider that carries it. It is a string, not an enum of everything the world
// supports: the template ships in_app (written locally) and email (the one
// external channel it configures). Any other value is data that a provider may
// accept, without gogogo growing a config surface or an address book for it.
//
// ChannelInApp is declared here for symmetry with the UI and the stored rows,
// but it is deliberately NOT a transport: it is written by the feature that
// owns the inbox, so a provider can never become a second owner of it.
type Channel string

// The channels the template itself creates. Other channel values (sms, chat,
// push) are legal data — they reach a provider that knows them, or are recorded
// skipped/unsupported — but they are not first-class here on purpose.
const (
	ChannelInApp Channel = "in_app"
	ChannelEmail Channel = "email"
)

// External reports whether ch is a channel a provider may carry. It is the
// guard that keeps in_app out of the transport loop.
func External(ch Channel) bool { return ch != ChannelInApp }

// Message is one outbound transport request. It carries no owner id beyond the
// opaque RecipientID the provider may need as a subscriber key, and the address
// it sends to; a provider must never be able to write to the app's store.
type Message struct {
	RecipientID   string
	To            string
	Subject       string
	Body          string
	TransactionID string
}

// Receipt is what a provider reports. Status mirrors the row vocabulary
// (sent/error/skipped) so the feature can record it without translating.
type Receipt struct {
	Provider   string
	Status     string
	SkipReason string
	Err        error
}

// Statuses a provider may report.
const (
	StatusSent    = "sent"
	StatusError   = "error"
	StatusSkipped = "skipped"
)

// Sender is one transport. Name is for the row's `provider` evidence and for
// logs; Send does the work.
type Sender interface {
	Name() string
	Send(ctx context.Context, m Message) (Receipt, error)
}

// Router answers "who carries this channel" once, at construction. A channel
// with no sender is a recorded skip, not a silent drop.
type Router struct {
	byChannel map[Channel]Sender
}

// NewRouter builds the router. A nil map is valid and means "no external
// channel configured", which is the default.
func NewRouter(senders map[Channel]Sender) *Router {
	if senders == nil {
		senders = map[Channel]Sender{}
	}
	return &Router{byChannel: senders}
}

// For returns the sender for ch.
func (r *Router) For(ch Channel) (Sender, bool) {
	if r == nil {
		return nil, false
	}
	s, ok := r.byChannel[ch]
	return s, ok
}

// Func adapts a plain send function to Sender. It is how the SMTP transport
// reuses internal/auth's mailer without this package importing auth.
type Func struct {
	provider string
	fn       func(ctx context.Context, m Message) error
}

// NewFunc builds a Sender from a function. A missing provider name or function
// is a wiring error, so it panics at boot rather than failing a delivery later.
func NewFunc(provider string, fn func(ctx context.Context, m Message) error) *Func {
	if provider == "" {
		panic("delivery: NewFunc with empty provider")
	}
	if fn == nil {
		panic("delivery: NewFunc with nil send func")
	}
	return &Func{provider: provider, fn: fn}
}

// Name returns the provider name recorded on the delivery row.
func (f *Func) Name() string { return f.provider }

// Send calls the wrapped function and reports a sent/error receipt.
func (f *Func) Send(ctx context.Context, m Message) (Receipt, error) {
	if err := f.fn(ctx, m); err != nil {
		return Receipt{Provider: f.provider, Status: StatusError, Err: err}, err
	}
	return Receipt{Provider: f.provider, Status: StatusSent}, nil
}
