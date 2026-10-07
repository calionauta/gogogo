// SCOPE:layer=infra,removal=plugin — GoAkt room grain tests.
package goakt

import (
	"context"
	"testing"
	"time"
)

func testHost(t *testing.T) (context.Context, *Host) {
	t.Helper()
	ctx, cancel := context.WithCancel(context.Background())
	t.Cleanup(cancel)
	h := New()
	h.expiry = 60 * time.Millisecond
	if err := h.Start(ctx); err != nil {
		t.Fatalf("host start: %v", err)
	}
	t.Cleanup(func() { _ = h.Stop(context.Background()) })
	return ctx, h
}

func waitFor(t *testing.T, d time.Duration, cond func() bool, what string) {
	t.Helper()
	deadline := time.Now().Add(d)
	for time.Now().Before(deadline) {
		if cond() {
			return
		}
		time.Sleep(10 * time.Millisecond)
	}
	t.Fatalf("timed out waiting for %s", what)
}

// TestRoomAcquireIsExclusive is THE property of the demo: two acquirers,
// exactly one winner. No KV store or pub/sub gives this; turn-based
// execution does.
func TestRoomAcquireIsExclusive(t *testing.T) {
	ctx, h := testHost(t)
	if err := h.Tell(ctx, "demo", RoomJoin{Name: "ana"}); err != nil {
		t.Fatalf("join: %v", err)
	}
	if err := h.Tell(ctx, "demo", RoomJoin{Name: "bob"}); err != nil {
		t.Fatalf("join: %v", err)
	}
	first, err := h.Acquire(ctx, "demo", "ana")
	if err != nil || !first.OK || first.Holder != "ana" {
		t.Fatalf("ana acquire = %+v, %v; want ok holder ana", first, err)
	}
	second, err := h.Acquire(ctx, "demo", "bob")
	if err != nil || second.OK || second.Holder != "ana" {
		t.Fatalf("bob acquire = %+v, %v; want rejected holder ana", second, err)
	}
	roster, err := h.Roster(ctx, "demo")
	if err != nil {
		t.Fatalf("roster: %v", err)
	}
	if roster.Presenter != "ana" || len(roster.Members) != 2 {
		t.Fatalf("roster = %+v; want presenter ana with 2 members", roster)
	}
}

// TestRoomCrashRecovers proves let-it-crash: the Crash hook panics, the
// supervisor restarts the room (generation bumps), and heartbeats rebuild
// the roster within seconds.
func TestRoomCrashRecovers(t *testing.T) {
	ctx, h := testHost(t)
	if err := h.Tell(ctx, "demo", RoomJoin{Name: "ana"}); err != nil {
		t.Fatalf("join: %v", err)
	}
	before, err := h.Roster(ctx, "demo")
	if err != nil {
		t.Fatalf("roster: %v", err)
	}
	_ = h.Tell(ctx, "demo", RoomCrash{})
	waitFor(t, 5*time.Second, func() bool {
		r, rErr := h.Roster(ctx, "demo")
		return rErr == nil && r.Generation > before.Generation
	}, "restart with bumped generation")
	// Roster is empty after restart (soft state); a heartbeat rebuilds it.
	if tellErr := h.Tell(ctx, "demo", RoomHeartbeat{Name: "ana"}); tellErr != nil {
		t.Fatalf("heartbeat: %v", tellErr)
	}
	after, err := h.Roster(ctx, "demo")
	if err != nil {
		t.Fatalf("roster: %v", err)
	}
	if len(after.Members) != 1 || after.Members[0].Name != "ana" {
		t.Fatalf("rebuilt roster = %+v; want ana only", after)
	}
}

// TestRoomRestartBudgetExhaustsFailClosed pins the far side of
// supervision: WithRetry(3) means the 4th fatal crash stops the room
// instead of restarting it forever. Queries must then fail fast with
// an error — never hang, never serve a zombie roster.
func TestRoomRestartBudgetExhaustsFailClosed(t *testing.T) {
	ctx, h := testHost(t)
	if err := h.Tell(ctx, "demo", RoomJoin{Name: "ana"}); err != nil {
		t.Fatalf("join: %v", err)
	}
	base, err := h.Roster(ctx, "demo")
	if err != nil {
		t.Fatalf("roster: %v", err)
	}
	// Three crashes consume the budget (generations 2, 3, 4); the
	// fourth exceeds it and the room stops.
	for i := range 4 {
		_ = h.Tell(ctx, "demo", RoomCrash{})
		want := base.Generation + i + 1
		if i == 3 {
			break
		}
		waitFor(t, 5*time.Second, func() bool {
			r, err := h.Roster(ctx, "demo")
			return err == nil && r.Generation >= want
		}, "restart budget consumption")
	}
	waitFor(t, 10*time.Second, func() bool {
		_, err := h.Roster(ctx, "demo")
		return err != nil
	}, "stopped room to fail queries")
}

// TestRoomStaleClaimsExpire pins lazy expiry: an idle presenter claim
// (past the test expiry) loses to the next acquirer.
func TestRoomStaleClaimsExpire(t *testing.T) {
	ctx, h := testHost(t)
	got, err := h.Acquire(ctx, "demo", "ana")
	if err != nil || !got.OK {
		t.Fatalf("ana acquire = %+v, %v", got, err)
	}
	time.Sleep(120 * time.Millisecond)
	next, err := h.Acquire(ctx, "demo", "bob")
	if err != nil || !next.OK || next.Holder != "bob" {
		t.Fatalf("bob acquire after expiry = %+v, %v; want ok holder bob", next, err)
	}
}

// TestSanitizeRoomID keeps actor names to stable path segments.
func TestSanitizeRoomID(t *testing.T) {
	for in, want := range map[string]string{
		"Team Alpha!": "teamalpha",
		"":            "lobby",
		"  ../x  ":    "x",
		"room-1":      "room-1",
	} {
		if got := sanitizeRoomID(in); got != want {
			t.Errorf("sanitize %q = %q, want %q", in, got, want)
		}
	}
}
