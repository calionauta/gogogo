// SCOPE:layer=feature,removal=feature — Room roster-read resilience tests.
package room

import (
	"context"
	"errors"
	"testing"
	"time"

	appcfg "github.com/calionauta/gogogo/config"
	appgoakt "github.com/calionauta/gogogo/internal/goakt"
)

// stubRooms is the fake roomSystem: the grain surface the handler needs,
// with call counting so the retry shape is observable.
type stubRooms struct {
	rosterCalls int
	rosterFn    func(call int) (appgoakt.Roster, error)
}

func (s *stubRooms) Roster(_ context.Context, _ string) (appgoakt.Roster, error) {
	s.rosterCalls++
	return s.rosterFn(s.rosterCalls)
}

func (s *stubRooms) Acquire(context.Context, string, string) (appgoakt.AcquireResult, error) {
	return appgoakt.AcquireResult{}, nil
}

func (s *stubRooms) Tell(context.Context, string, any) error { return nil }

// TestReadRosterRetriesTransientFailure pins the supervised-restart
// tolerance: a read that fails once must not surface as a 502 when the next
// attempt succeeds. The grain restart window (and a starved runner) is
// transient by nature; Roster is a pure read, so retrying it is free of
// side effects.
func TestReadRosterRetriesTransientFailure(t *testing.T) {
	rooms := &stubRooms{
		rosterFn: func(call int) (appgoakt.Roster, error) {
			if call == 1 {
				return appgoakt.Roster{}, errors.New("transient grain stall")
			}
			return appgoakt.Roster{Presenter: "ana@example.com"}, nil
		},
	}
	h := New(&appcfg.Config{}, rooms)

	got, err := h.readRoster(context.Background(), "lobby")
	if err != nil {
		t.Fatalf("readRoster after one transient failure: %v", err)
	}
	if rooms.rosterCalls != 2 {
		t.Errorf("roster calls = %d, want 2 (one failure + one retry)", rooms.rosterCalls)
	}
	if got.Presenter != "ana@example.com" {
		t.Errorf("roster = %+v, want the successful retry's answer", got)
	}
}

// TestReadRosterFailsBoundedOnPermanentError pins the other side: a room
// that is genuinely down (supervisor restart budget exhausted) must still
// fail, with a bounded number of attempts — never retry forever and never
// hang the request.
func TestReadRosterFailsBoundedOnPermanentError(t *testing.T) {
	rooms := &stubRooms{
		rosterFn: func(int) (appgoakt.Roster, error) {
			return appgoakt.Roster{}, errors.New("room stopped")
		},
	}
	h := New(&appcfg.Config{}, rooms)
	h.readBudget = 200 * time.Millisecond

	start := time.Now()
	if _, err := h.readRoster(context.Background(), "lobby"); err == nil {
		t.Fatal("readRoster on a permanently failing grain returned nil error")
	}
	if elapsed := time.Since(start); elapsed > time.Second {
		t.Errorf("readRoster took %v; the read budget must bound it", elapsed)
	}
	if rooms.rosterCalls > 3 {
		t.Errorf("roster calls = %d, want at most 3", rooms.rosterCalls)
	}
}
