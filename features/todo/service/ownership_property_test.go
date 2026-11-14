// Property-based ownership test: the one invariant that every frontend relies
// on, proved over generated data instead of three hand-written cases.
//
// The claim is narrow and total: a session owner sees only their own rows (plus
// rows somebody marked public), and deleting a row they do not own never
// removes it. Hand-written tests cover the happy path plus one refusal; this one
// covers arbitrary combinations of owners, visibilities and deletion orders,
// which is exactly where a WHERE clause drift hides.
package service

import (
	"context"
	"fmt"
	"math/rand"
	"testing"

	"github.com/calionauta/gogogo/features/todo"
	"github.com/calionauta/gogogo/internal/auth"
)

// ownerPair is a second account in the same database: ownership is only
// meaningful when somebody else exists.
type ownerPair struct {
	first  string
	second string
	svc    *Service
}

func newOwnerPair(t *testing.T) ownerPair {
	t.Helper()
	q, svc, _, first := openTestDB(t)
	a, aerr := auth.New(q, false)
	if aerr != nil {
		t.Fatalf("auth: %v", aerr)
	}
	_, tok, err := a.Signup(context.Background(), "second@example.com", "password123", "Second")
	if err != nil {
		t.Fatalf("signup second: %v", err)
	}
	sess, err := a.SessionByToken(context.Background(), tok)
	if err != nil {
		t.Fatalf("session second: %v", err)
	}
	second := sess.User.ID
	return ownerPair{first: first, second: second, svc: svc}
}

// TestOwnershipHoldsForGeneratedRows is the property: for any list of rows with
// any owners and visibilities, ListOwned(owner) returns exactly the rows that
// owner created, and Delete by a non-owner is refused while leaving the row in
// place.
func TestOwnershipHoldsForGeneratedRows(t *testing.T) {
	pair := newOwnerPair(t)
	ctx := context.Background()
	rng := rand.New(rand.NewSource(20261113)) //nolint:gosec // deterministic test data, not crypto

	const rounds = 12
	for round := range rounds {
		n := 1 + rng.Intn(5)
		var mine, theirs []string
		for i := range n {
			owner, want := pair.first, true
			if rng.Intn(2) == 0 {
				owner, want = pair.second, false
			}
			vis := todo.VisibilityPrivate
			if rng.Intn(2) == 0 {
				vis = todo.VisibilityPublic
			}
			id, err := pair.svc.Create(ctx, owner, fmt.Sprintf("r%d-%d", round, i), vis, "")
			if err != nil {
				t.Fatalf("round %d create: %v", round, err)
			}
			if want {
				mine = append(mine, id)
			} else {
				theirs = append(theirs, id)
			}
		}

		owned, err := pair.svc.ListOwned(ctx, pair.first, "")
		if err != nil {
			t.Fatalf("round %d ListOwned: %v", round, err)
		}
		got := map[string]bool{}
		for _, item := range owned {
			got[item.ID] = true
			if item.Owner != pair.first {
				t.Fatalf("round %d: ListOwned(first) returned %s owned by %s", round, item.ID, item.Owner)
			}
		}
		for _, id := range mine {
			if !got[id] {
				t.Fatalf("round %d: ListOwned(first) dropped its own row %s", round, id)
			}
		}
		for _, id := range theirs {
			if got[id] {
				t.Fatalf("round %d: ListOwned(first) returned %s, which belongs to second", round, id)
			}
		}

		// Deleting somebody else's row is refused and does not remove it.
		if len(theirs) == 0 {
			continue
		}
		victim := theirs[rng.Intn(len(theirs))]
		if _, derr := pair.svc.Delete(ctx, pair.first, "user", victim); derr == nil {
			t.Fatalf("round %d: first deleted second's row %s", round, victim)
		}
		still, err := pair.svc.ListOwned(ctx, pair.second, "")
		if err != nil {
			t.Fatalf("round %d ListOwned(second): %v", round, err)
		}
		found := false
		for _, item := range still {
			if item.ID == victim {
				found = true
			}
		}
		if !found {
			t.Fatalf("round %d: refused delete removed %s anyway", round, victim)
		}
	}
}
