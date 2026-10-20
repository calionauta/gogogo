// SCOPE:plugin — GoAkt room grain tests.
package goakt

import (
	"context"
	"slices"
	"sort"
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

// TestRoomRosterOrderIsStable pins the roster's presentation contract:
// two members must come back in the SAME order on every read. The roster
// used to be built by ranging a map, and Go randomizes map iteration per
// range, so the list reshuffled between polls (the reported "sometimes
// demo1 is first, sometimes demo2") and, since the page re-renders every
// 3s, it visibly flickered even when nothing changed.
//
// RED-PROOFED: drop the sort.Slice in roster() and this fails within a
// handful of reads.
func TestRoomRosterOrderIsStable(t *testing.T) {
	ctx, h := testHost(t)
	for _, name := range []string{"ana", "bob", "cleo", "dan", "eve"} {
		if err := h.Tell(ctx, "order", RoomJoin{Name: name}); err != nil {
			t.Fatalf("join %s: %v", name, err)
		}
	}
	first, err := h.Roster(ctx, "order")
	if err != nil {
		t.Fatalf("roster: %v", err)
	}
	want := make([]string, 0, len(first.Members))
	for _, m := range first.Members {
		want = append(want, m.Name)
	}
	if !sort.StringsAreSorted(want) {
		t.Fatalf("roster is not name-ordered: %v", want)
	}
	// Repeat reads must be byte-identical in order, not just sorted-once.
	for i := range 25 {
		r, rErr := h.Roster(ctx, "order")
		if rErr != nil {
			t.Fatalf("roster read %d: %v", i, rErr)
		}
		got := make([]string, 0, len(r.Members))
		for _, m := range r.Members {
			got = append(got, m.Name)
		}
		if !slices.Equal(got, want) {
			t.Fatalf("roster read %d = %v, want stable %v", i, got, want)
		}
	}
}

// TestRoomReacquireByHolderSucceeds pins the other half of the lock's
// honesty: a member who already holds the presenter lock must get OK back
// when they take it again. Reporting failure made "Take presenter" a dead
// button for the one person who had already won.
func TestRoomReacquireByHolderSucceeds(t *testing.T) {
	ctx, h := testHost(t)
	if _, err := h.Acquire(ctx, "reacquire", "ana"); err != nil {
		t.Fatalf("first acquire: %v", err)
	}
	again, err := h.Acquire(ctx, "reacquire", "ana")
	if err != nil || !again.OK || again.Holder != "ana" {
		t.Fatalf("holder re-acquire = %+v, %v; want ok holder ana", again, err)
	}
	// A different member still loses, naming the real holder.
	other, err := h.Acquire(ctx, "reacquire", "bob")
	if err != nil || other.OK || other.Holder != "ana" {
		t.Fatalf("bob acquire = %+v, %v; want rejected holder ana", other, err)
	}
}

// TestRoomLeaveDropsMemberAndPresenter pins Leave's full effect: the
// member leaves the roster AND loses the lock. The page used to re-add
// them on the next heartbeat (see TestRoomHeartbeatAfterLeaveRejoins);
// the grain side must at least drop both facts immediately.
func TestRoomLeaveDropsMemberAndPresenter(t *testing.T) {
	ctx, h := testHost(t)
	if err := h.Tell(ctx, "leave", RoomJoin{Name: "ana"}); err != nil {
		t.Fatalf("join: %v", err)
	}
	if got, err := h.Acquire(ctx, "leave", "ana"); err != nil || !got.OK {
		t.Fatalf("acquire: %+v, %v", got, err)
	}
	if err := h.Tell(ctx, "leave", RoomLeave{Name: "ana"}); err != nil {
		t.Fatalf("leave: %v", err)
	}
	roster, err := h.Roster(ctx, "leave")
	if err != nil {
		t.Fatalf("roster: %v", err)
	}
	if len(roster.Members) != 0 {
		t.Fatalf("roster after leave = %+v, want empty", roster.Members)
	}
	if roster.Presenter != "" {
		t.Fatalf("presenter after leave = %q, want empty", roster.Presenter)
	}
}

// TestRoomTwoTabsOneMemberSurvivesOneClosing pins the refcount contract
// that makes SSE presence safe: presence is per CONNECTION, the roster is
// per NAME. Closing one of two tabs of the same person must not evict them
// (nor drop the lock they hold), and only closing the LAST connection may.
//
// This is exactly the failure a heartbeat cannot have and an SSE stream can:
// the stream's disconnect is the leave signal, so without the refcount a
// user vanished the moment they closed any one of their tabs.
//
// RED-PROOFED: drop the hasConns guard in leave() and the first half fails
// (the member disappears while their second tab is still connected).
func TestRoomTwoTabsOneMemberSurvivesOneClosing(t *testing.T) {
	ctx, h := testHost(t)

	// One person, two tabs.
	for _, tab := range []string{"tab-a", "tab-b"} {
		if err := h.Tell(ctx, "tabs", RoomJoin{Name: "ana", ClientID: tab}); err != nil {
			t.Fatalf("join %s: %v", tab, err)
		}
	}
	if err := h.Tell(ctx, "tabs", RoomAcquire{Name: "ana", ClientID: "tab-a"}); err != nil {
		t.Fatalf("acquire from tab-a: %v", err)
	}

	roster, err := h.Roster(ctx, "tabs")
	if err != nil {
		t.Fatalf("roster: %v", err)
	}
	if len(roster.Members) != 1 {
		t.Fatalf("two tabs of one person = %d member(s), want 1", len(roster.Members))
	}

	// One tab closes: the person is still here, lock included.
	if err := h.Tell(ctx, "tabs", RoomLeave{Name: "ana", ClientID: "tab-a"}); err != nil {
		t.Fatalf("leave tab-a: %v", err)
	}
	roster, readErr := h.Roster(ctx, "tabs")
	if readErr != nil {
		t.Fatalf("roster after one tab left: %v", readErr)
	}
	if len(roster.Members) != 1 {
		t.Fatalf("member vanished while a second tab was connected: %+v", roster.Members)
	}
	if roster.Presenter != "ana" {
		t.Fatalf("lock dropped while the holder still had a tab open: %q", roster.Presenter)
	}

	// The last tab closes: now they leave, and the lock goes with them.
	if err := h.Tell(ctx, "tabs", RoomLeave{Name: "ana", ClientID: "tab-b"}); err != nil {
		t.Fatalf("leave tab-b: %v", err)
	}
	roster, readErr = h.Roster(ctx, "tabs")
	if readErr != nil {
		t.Fatalf("roster after last tab left: %v", readErr)
	}
	if len(roster.Members) != 0 {
		t.Fatalf("member stayed after their last connection closed: %+v", roster.Members)
	}
	if roster.Presenter != "" {
		t.Fatalf("lock survived its holder's last connection: %q", roster.Presenter)
	}
}

// TestRoomStaleConnectionExpiresButMemberSurvives pins the other side of the
// refcount: a connection that goes quiet past the expiry is dropped (a
// crashed client that never sent FIN), while a member who still has a live
// connection stays. Expiry is per connection, never per member.
func TestRoomStaleConnectionExpiresButMemberSurvives(t *testing.T) {
	ctx, h := testHost(t)
	if err := h.Tell(ctx, "stale", RoomJoin{Name: "ana", ClientID: "fresh"}); err != nil {
		t.Fatalf("join fresh: %v", err)
	}
	if err := h.Tell(ctx, "stale", RoomJoin{Name: "ana", ClientID: "stale"}); err != nil {
		t.Fatalf("join stale: %v", err)
	}
	// Let the test expiry pass, then re-confirm only the surviving tab.
	time.Sleep(120 * time.Millisecond)
	if err := h.Tell(ctx, "stale", RoomHeartbeat{Name: "ana", ClientID: "fresh"}); err != nil {
		t.Fatalf("heartbeat fresh: %v", err)
	}
	roster, err := h.Roster(ctx, "stale")
	if err != nil {
		t.Fatalf("roster: %v", err)
	}
	if len(roster.Members) != 1 || roster.Members[0].Name != "ana" {
		t.Fatalf("roster = %+v, want ana once (one live connection)", roster.Members)
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
