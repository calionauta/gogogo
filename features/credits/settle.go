// Package credits — settle path. Split from biller.go because the recovery
// path (re-charging a reservation that outlived its lease) is what makes this
// file longer than a closure.
package credits

import (
	"context"
	"errors"
	"fmt"
	"log/slog"
	"strings"

	"github.com/calionauta/ai-credits/credits"

	"github.com/zendev-sh/goai/provider"
)

// settleLedgerReason is the ledger reason recorded when a reservation has to be
// re-charged outside its original settle. It is the audit trail for the only
// path that can debit a user after the request that caused it has finished.
const settleLedgerReason = "settle_after_lease_expiry"

// settleGap is the idempotency-key prefix for the recovery debit. One call is
// charged at most once even if recovery runs twice (two tickers, a retry, a
// restart): the ledger's UNIQUE key rejects the second write.
const settleGap = "settle-gap:"

// settleCall charges the real usage for one generated response.
//
// Two things here are load-bearing and were both wrong before:
//
//  1. The reservation lease is 5 minutes and Reconcile expires stale
//     reservations every 10. A response slower than the lease therefore lands
//     on a reservation that has already been REFUNDED, and Settle's CAS returns
//     ErrReservationClosed. The old code discarded that error (`_ = err`), so
//     the user kept the answer and paid nothing. The ledger stayed consistent —
//     the adjustment and the failed CAS share one tx and roll back together —
//     which is exactly why nobody noticed: it is revenue loss, not corruption.
//     Any non-nil error from Settle is now handled instead of dropped.
//
//  2. Recovery must be idempotent. The debit goes through the ledger's
//     idempotency key, so a second attempt (crash mid-recovery, overlapping
//     tickers) cannot bill twice. Without the key this "fix" would trade a
//     missed charge for a double charge.
func settleCall(ctx context.Context, s *Service, rsv *credits.Reservation, model string, u provider.Usage) {
	rec := fromGoAIUsage("", "", model, u) // model + tokens drive the cost
	if !hasTokens(u) {
		releaseQuietly(ctx, s, rsv, "no_tokens")
		return
	}
	cost, err := s.Credits.Credits(ctx, rec)
	if err != nil {
		releaseQuietly(ctx, s, rsv, "cost_error")
		slog.ErrorContext(ctx, "credits: settle aborted, cannot compute cost",
			"reservation_id", rsv.ID, "user_id", rsv.UserID, "model", model,
			"tokens_in", u.InputTokens, "tokens_out", u.OutputTokens, "error", err)
		return
	}

	err = s.Credits.Settle(ctx, rsv, cost)
	switch {
	case err == nil:
		// Settled directly, so the outbox row EnqueueSettlement wrote a moment
		// ago still says "pending". Left alone, ProcessSettlementOutbox re-settles
		// the closed reservation on its next tick, records the failure, and the
		// charge shows up as a permanent phantom hole. Closing the row here is the
		// other half of the direct path — not an optional cleanup.
		if closeErr := s.Credits.MarkOutboxSettled(ctx, rsv.RequestID); closeErr != nil {
			// The charge is captured; this only affects the audit row. Keep the
			// failure visible without failing a request that was billed correctly.
			slog.ErrorContext(ctx, "credits: settled, but the outbox row was not closed; "+
				"the drainer will report this charge as a failed settlement",
				"reservation_id", rsv.ID, "request_id", rsv.RequestID, "user_id", rsv.UserID,
				"error", closeErr)
		}
		return
	case errors.Is(err, credits.ErrReservationClosed):
		// The lease expired before the response finished: the reserve was
		// already refunded, so the captured amount was never taken. Charge the
		// real cost as a separate, idempotent ledger debit.
		recoverExpiredSettle(ctx, s, rsv, cost, model)
	default:
		// Unknown failure. Leave the reservation open (Reconcile will expire it
		// and refund) and make the loss visible: a silent branch here is what
		// let the original bug live for so long.
		slog.ErrorContext(ctx, "credits: settle failed, leaving reservation to Reconcile",
			"reservation_id", rsv.ID, "user_id", rsv.UserID, "model", model,
			"credits", cost, "error", err)
	}
}

// recoverExpiredSettle debits a reservation whose lease lapsed before Settle
// could capture it. Best effort by construction: the response is already
// delivered and the caller has moved on, so a failure here is logged for an
// operator rather than returned to nobody.
func recoverExpiredSettle(ctx context.Context, s *Service, rsv *credits.Reservation, cost int64, model string) {
	if cost <= 0 {
		return
	}
	// context.WithoutCancel: the request's context is usually already done by
	// the time a slow response settles. A cancelled context would make this
	// debit fail for the exact case it exists to handle.
	ctx = context.WithoutCancel(ctx)

	key := fmt.Sprintf("%s%s", settleGap, rsv.ID)
	err := s.Credits.Refund(ctx, rsv.UserID, cost, "goai", settleLedgerReason, key)
	switch {
	case err == nil:
		slog.WarnContext(ctx, "credits: charged a call whose reservation lease expired",
			"reservation_id", rsv.ID, "user_id", rsv.UserID, "model", model,
			"credits", cost, "reason", settleLedgerReason)
	case errors.Is(err, credits.ErrDuplicateGrant) || isDuplicateLedgerKey(err):
		// Already recovered (retry, restart, second ticker). Correct outcome.
		slog.DebugContext(ctx, "credits: settle recovery already applied",
			"reservation_id", rsv.ID, "idempotency_key", key)
	default:
		slog.ErrorContext(ctx, "credits: settle recovery failed, call went unbilled",
			"reservation_id", rsv.ID, "user_id", rsv.UserID, "model", model,
			"credits", cost, "idempotency_key", key, "error", err)
	}
}

// isDuplicateLedgerKey is the backstop for drivers that surface the UNIQUE
// violation instead of the library's sentinel. The sentinel
// (credits.ErrDuplicateGrant) is checked first and is the normal path.
func isDuplicateLedgerKey(err error) bool {
	msg := err.Error()
	return strings.Contains(msg, "idempotency_key") || strings.Contains(msg, "UNIQUE constraint failed")
}

// releaseQuietly returns the whole reserve when nothing is owed. Failures are
// logged, not swallowed: a release that never lands keeps the user's credits
// held until Reconcile catches it, and an operator deserves to know when the
// safety net is doing the work.
func releaseQuietly(ctx context.Context, s *Service, rsv *credits.Reservation, why string) {
	if err := s.Credits.Release(ctx, rsv); err != nil && !errors.Is(err, credits.ErrReservationClosed) {
		slog.WarnContext(ctx, "credits: release failed, Reconcile will expire the reservation",
			"reservation_id", rsv.ID, "user_id", rsv.UserID, "why", why, "error", err)
	}
}
