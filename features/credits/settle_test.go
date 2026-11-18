package credits

import (
	"context"
	"testing"

	"github.com/zendev-sh/goai/provider"

	"github.com/calionauta/ai-credits/credits"

	"github.com/calionauta/gogogo/internal/llm"
)

// TestSettleChargesWhenReservationLeaseExpired is the regression guard for the
// silent unbilled-call bug (found by model checking BillingLease.tla).
//
// The reservation lease is 5 minutes; Reconcile expires stale reservations every
// 10. A response that finishes after its lease was expired lands on an
// already-refunded reservation, so Settle's CAS returns ErrReservationClosed.
// settleCall used to discard that error (`_ = err`), which meant: the user is
// charged nothing for a response they received, the ledger stays perfectly
// consistent (the adjustment and the failed CAS share one transaction and roll
// back together), and nothing anywhere logs a word about it.
//
// The interleaving, in the same order the checked model produces it:
//
//	Reserve → lease burns down → Reconcile expires+refunds → response arrives
//
// RED-proofed: with the discarded error this fails with "balance = 1000" — the
// full monthly grant, untouched, after a real 150-token response. With the
// recovery path it charges the real cost exactly once.
func TestSettleChargesWhenReservationLeaseExpired(t *testing.T) {
	s := newTestService(t)
	ctx := llm.WithUserID(context.Background(), "user-1")

	// Reserve, then expire the lease deliberately — exactly what a too-slow
	// response looks like from the ledger's side.
	rsv := reserveAndExpire(ctx, t, s)
	before, err := s.Credits.Balance(ctx, "user-1")
	if err != nil {
		t.Fatal(err)
	}

	// The response finally arrives with real usage.
	settleCall(ctx, s, rsv, "fake-1", provider.Usage{InputTokens: 100, OutputTokens: 50})

	after, err := s.Credits.Balance(ctx, "user-1")
	if err != nil {
		t.Fatal(err)
	}
	if after >= before {
		t.Fatalf("balance after a settled response that outlived its reservation = %d (was %d): "+
			"the call was not charged. Settle returned ErrReservationClosed because Reconcile "+
			"expired the lease first, and that error was discarded — revenue lost, ledger still "+
			"consistent, nothing logged", after, before)
	}
}

// TestSettleRecoveryIsIdempotent guards the other half of the same fix: the
// recovery debit must not bill twice when it runs more than once (a retry, a
// restart mid-recovery, overlapping tickers). The ledger's idempotency key is
// what makes repeating it safe.
func TestSettleRecoveryIsIdempotent(t *testing.T) {
	s := newTestService(t)
	ctx := llm.WithUserID(context.Background(), "user-1")

	rsv := reserveAndExpire(ctx, t, s)

	settleCall(ctx, s, rsv, "fake-1", provider.Usage{InputTokens: 100, OutputTokens: 50})
	once, err := s.Credits.Balance(ctx, "user-1")
	if err != nil {
		t.Fatal(err)
	}

	// Replay the recovery path, as a second ticker or a post-crash retry would.
	recoverExpiredSettle(ctx, s, rsv, 7, "fake-1")

	twice, err := s.Credits.Balance(ctx, "user-1")
	if err != nil {
		t.Fatal(err)
	}
	if twice != once {
		t.Fatalf("balance changed on a replayed recovery debit: %d -> %d. The debit must be "+
			"keyed (settle-gap:<reservation id>) so the ledger's UNIQUE constraint makes a "+
			"second attempt a no-op — otherwise recovery trades a missed charge for a double charge",
			once, twice)
	}
}

// reserveAndExpire makes a reservation and then releases it, which is the state
// Reconcile leaves behind when it expires a lease: status closed, credits
// refunded, and the caller's *Reservation still claims "reserved". That stale
// struct is what a slow response settles against.
func reserveAndExpire(ctx context.Context, t *testing.T, s *Service) *credits.Reservation {
	t.Helper()
	rsv, err := s.Credits.Reserve(ctx, "user-1", credits.NewRequestID(), 100)
	if err != nil {
		t.Fatalf("Reserve: %v", err)
	}
	stale := *rsv // the caller's copy, unaware the lease is about to lapse
	if err := s.Credits.Release(ctx, rsv); err != nil {
		t.Fatalf("expire the lease: %v", err)
	}
	return &stale
}

// TestSuccessfulSettleClosesItsOutboxRow is the gogogo-side half of
// ai-credits' TestDirectSettleLeavesNoPhantomOutboxRow.
//
// The biller enqueues a settlement row before the request runs, so a crash
// mid-request leaves a drainable trace. When the request succeeds and
// settleCall settles DIRECTLY (it has real usage in hand), that row kept saying
// "pending" while the charge was already captured. The next drain tick then
// re-settled a closed reservation, got ErrReservationClosed, and stamped
// status='failed' — one phantom billing failure per successful request, forever.
//
// RED-proofed: dropping the MarkOutboxSettled call in settleCall leaves the row
// "pending" and this fails on the first assertion.
func TestSuccessfulSettleClosesItsOutboxRow(t *testing.T) {
	s, db := newTestServiceWithDB(t)
	ctx := llm.WithUserID(context.Background(), "user-1")

	rsv, err := s.Credits.Reserve(ctx, "user-1", credits.NewRequestID(), 100)
	if err != nil {
		t.Fatalf("Reserve: %v", err)
	}
	// The biller's enqueue, same shape and same moment: before the call runs.
	if err := s.Credits.EnqueueSettlement(ctx, rsv.RequestID, "user-1", rsv.ID, "goai", "fake-1"); err != nil {
		t.Fatalf("EnqueueSettlement: %v", err)
	}

	settleCall(ctx, s, rsv, "fake-1", provider.Usage{InputTokens: 100, OutputTokens: 50})

	var status string
	if err := db.QueryRowContext(ctx,
		`SELECT status FROM settlement_outbox WHERE request_id=?`, rsv.RequestID).Scan(&status); err != nil {
		t.Fatalf("read outbox row: %v", err)
	}
	if status != "settled" {
		t.Fatalf("outbox status = %q, want \"settled\": the direct settle path captured "+
			"the charge but left the row open, so the drainer will re-settle a closed "+
			"reservation and report a billing failure that never happened", status)
	}
}
