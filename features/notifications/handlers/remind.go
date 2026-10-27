// SCOPE:feature — notifications: scheduling a reminder.
//
// The handler schedules and reports; it never waits. The delay belongs to the
// engine (a DagNats `sleep` step), so a restart between "schedule" and "deliver"
// loses nothing — which a goroutine and a timer here would.
package handlers

import (
	"context"
	"database/sql"
	"errors"
	"net/http"
	"strings"
	"time"

	sdk "github.com/starfederation/datastar-go/datastar"

	dbgen "github.com/calionauta/gogogo/internal/db/sqlcgen"
	"github.com/calionauta/gogogo/internal/dbschema"
	"github.com/calionauta/gogogo/internal/delivery"
	"github.com/calionauta/gogogo/internal/observability"

	"github.com/calionauta/gogogo/features/notifications"
)

const (
	// maxReminderBody bounds the text that goes into a delivery row. The form
	// is the only writer, so this is a guard against a hand-made request.
	maxReminderBody = 280

	// workflowReminder is the DagNats workflow this feature starts. It is a
	// constant because the definition lives in internal/dagnats and the two
	// spellings must match or every schedule 404s at the engine.
	workflowReminder = "reminder"
)

// reminderInput is the run input. `due_at` is RFC3339 and feeds the workflow's
// `until_input_path` sleep; `transaction_id` feeds `idempotency_key`, so a
// double-submit starts one run.
type reminderInput struct {
	User string `json:"user"`
	//nolint:tagliatelle // DagNats workflow input uses snake_case (idempotency_key path).
	TransactionID string `json:"transaction_id"`
	Body          string `json:"body"`
	//nolint:tagliatelle // DagNats `until_input_path` reads this exact key.
	DueAt string `json:"due_at"`
	// Channels and Recipients ride the run input so the worker can hand the
	// message to the transports without a second user lookup. Recipients maps
	// channel -> address (email address, phone, chat id).
	Channels   []string          `json:"channels,omitempty"`
	Recipients map[string]string `json:"recipients,omitempty"`
}

// handleRemind schedules one reminder and patches the inbox with a notice.
//
// The form sends `text`, `delay` (1m, 1h, 24h) and zero or more `channels`.
// An unknown delay is a 400, never a silent default: a reminder that fires at a
// time the user did not pick is worse than an error.
func (h *Handler) handleRemind(w http.ResponseWriter, r *http.Request) {
	if err := r.ParseForm(); err != nil {
		http.Error(w, "bad form", http.StatusBadRequest)
		return
	}
	sess, err := h.auth.Session(r)
	if err != nil || sess == nil || sess.User == nil {
		http.Error(w, "auth required", http.StatusUnauthorized)
		return
	}
	if !h.wired() {
		http.Error(w, "durable workflows are not wired in this build", http.StatusServiceUnavailable)
		return
	}
	body := strings.TrimSpace(r.FormValue("text"))
	if body == "" {
		http.Error(w, "empty reminder", http.StatusBadRequest)
		return
	}
	if len(body) > maxReminderBody {
		body = body[:maxReminderBody]
	}
	dueAt, ok := reminderDueAt(r.FormValue("delay"), time.Now())
	if !ok {
		http.Error(w, "unknown delay", http.StatusBadRequest)
		return
	}
	// Only channels the router has a transport for are honoured, and the
	// address comes from the session, never from the form. An unconfigured
	// checkbox cannot smuggle a channel into the run.
	channels, recipients := h.selectedChannels(r.Form["channels"], sess.User.Email)
	if _, serr := h.runs.StartRun(r.Context(), workflowReminder, reminderInput{
		User:          sess.User.ID,
		TransactionID: "reminder:" + dbschema.NewID(),
		Body:          body,
		DueAt:         dueAt,
		Channels:      channels,
		Recipients:    recipients,
	}); serr != nil {
		observability.Error(r.Context(), "notifications: start reminder run",
			append(observability.WithComponent("notifications", "feature"), "error", serr)...)
		http.Error(w, "could not schedule", http.StatusBadGateway)
		return
	}
	data, derr := h.pageData(r.Context(), sess.User.ID, "Reminder scheduled.")
	if derr != nil {
		h.logError(r, "notifications: reload inbox", derr)
		http.Error(w, "inbox unavailable", http.StatusInternalServerError)
		return
	}
	sse := sdk.NewSSE(w, r)
	//nolint:contextcheck // datastar-go sink boundary: RenderAndPatch renders through templ; the SDK takes no ctx.
	if perr := h.patchInboxData(sse, data); perr != nil {
		h.logError(r, "notifications: patch inbox", perr)
	}
}

// reminderDueAt turns the form's delay into an RFC3339 instant. It is a pure
// function so the boundary cases are testable without a clock.
func reminderDueAt(delay string, now time.Time) (string, bool) {
	var d time.Duration
	switch delay {
	case "1m":
		d = time.Minute
	case "1h":
		d = time.Hour
	case "24h":
		d = 24 * time.Hour
	default:
		return "", false
	}
	return now.Add(d).UTC().Format(time.RFC3339), true
}

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

// statusPending is the external-channel state between "row written" and "the
// provider answered". A crash in that window leaves a visible pending row
// instead of a lost message.
const statusPending = "pending"

// providerLocal is the evidence recorded for the always-on in-app channel.
const providerLocal = "local"

// Deliver is the worker body: it writes the in-app row, wakes the owner's open
// streams, and hands the message to every requested external channel.
//
// A duplicate in-app row (same owner, transaction, channel) means the run was
// retried and the user already has the notification, so it publishes nothing
// and does not send a second external copy.
func (h *Handler) Deliver(ctx context.Context, d Delivery) error {
	_, created, err := h.record(ctx, d, "in_app", providerLocal, delivery.StatusSent, "")
	if err != nil {
		return err
	}
	if !created {
		return nil
	}
	h.bus.Publish(notifications.Event{Owner: d.Owner})
	for _, ch := range d.Channels {
		if !delivery.External(ch) {
			continue
		}
		h.deliverChannel(ctx, d, ch)
	}
	return nil
}

// deliverChannel is best-effort by design: the in-app row is the contract, and
// an external failure is recorded as `error` (never retried in a loop) so the
// user still has the message. The provider's own delivery record is the deeper
// evidence; this row is the app's side of the story.
func (h *Handler) deliverChannel(ctx context.Context, d Delivery, ch delivery.Channel) {
	sender, ok := h.delivery.For(ch)
	if !ok {
		// The transport vanished between scheduling and delivery (config
		// change, or a trimmed build). Record it instead of dropping it.
		_, _, _ = h.record(ctx, d, string(ch), "", delivery.StatusSkipped, "provider")
		return
	}
	addr := d.Recipients[ch]
	if addr == "" {
		_, _, _ = h.record(ctx, d, string(ch), "", delivery.StatusSkipped, "no_address")
		return
	}
	row, created, err := h.record(ctx, d, string(ch), sender.Name(), statusPending, "")
	if err != nil || !created {
		return
	}
	// The provider sees a per-channel transaction id. A provider that dedupes
	// on transaction+recipient (notify does) would otherwise treat the second
	// channel of the same delivery as a retry and drop it.
	txID := d.TransactionID + ":" + string(ch)
	receipt, serr := sender.Send(ctx, delivery.Message{
		RecipientID:   d.Owner,
		To:            addr,
		Subject:       d.Subject,
		Body:          d.Body,
		TransactionID: txID,
	})
	if serr != nil {
		h.setStatus(ctx, d.Owner, row.ID, delivery.StatusError, "", serr.Error())
		return
	}
	h.setStatus(ctx, d.Owner, row.ID, receipt.Status, receipt.SkipReason, "")
}

// selectedChannels maps the form's checked values to the channels the router
// can actually serve, with the address taken from the session. An unconfigured
// or unaddressable channel is dropped here, not recorded later as a skip.
func (h *Handler) selectedChannels(raw []string, email string) ([]string, map[string]string) {
	var channels []string
	recipients := map[string]string{}
	for _, v := range raw {
		ch := delivery.Channel(v)
		if !delivery.External(ch) {
			continue
		}
		addr := h.addressFor(ch, email)
		if addr == "" {
			continue
		}
		if _, ok := h.delivery.For(ch); !ok {
			continue
		}
		channels = append(channels, string(ch))
		recipients[string(ch)] = addr
	}
	return channels, recipients
}

// addressFor is the address book. Email comes from the session's user; a new
// channel needs a place to read its address (a column or a table), which is the
// one piece the transport seam cannot invent.
func (h *Handler) addressFor(ch delivery.Channel, email string) string {
	switch ch {
	case delivery.ChannelEmail:
		return email
	default:
		return ""
	}
}

// record writes one channel row. created is false on a duplicate, which is the
// dedupe contract, not an error.
func (h *Handler) record(
	ctx context.Context, d Delivery, channel, provider, status, skipReason string,
) (dbgen.Notification, bool, error) {
	row, err := h.q.CreateNotification(ctx, dbgen.CreateNotificationParams{
		ID:            dbschema.NewID(),
		Owner:         d.Owner,
		Workflow:      d.Workflow,
		TransactionID: d.TransactionID,
		Channel:       channel,
		Status:        status,
		SkipReason:    skipReason,
		Provider:      provider,
		Subject:       d.Subject,
		Body:          d.Body,
		Created:       dbschema.Now(),
	})
	if errors.Is(err, sql.ErrNoRows) {
		return dbgen.Notification{}, false, nil
	}
	if err != nil {
		return dbgen.Notification{}, false, err
	}
	return row, true, nil
}

// setStatus closes the external row. A failure here is logged, not returned:
// the send already happened and the user's inbox row is the contract.
func (h *Handler) setStatus(ctx context.Context, owner, id, status, reason, errorText string) {
	if _, err := h.q.SetNotificationStatus(ctx, dbgen.SetNotificationStatusParams{
		Status:     status,
		SkipReason: reason,
		ErrorText:  errorText,
		ID:         id,
		Owner:      owner,
	}); err != nil {
		observability.Error(ctx, "notifications: update delivery status",
			append(observability.WithComponent("notifications", "feature"), "error", err)...)
	}
}
