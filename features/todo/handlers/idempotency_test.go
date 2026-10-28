// SCOPE:feature — idempotent replay of a queued mutation.
//
// A queued write is delivered at least once: the response can be lost after
// the server committed, and the outbox will send the SAME intent again. A
// toggle is a read-then-flip, so a second delivery used to flip the row back —
// silently reverting the user's change long after they made it.
//
// The receipt (op_receipts, keyed by owner + Idempotency-Key) is claimed in the
// same transaction as the mutation, so "applied" and "recorded" cannot
// disagree. These tests drive the HTTP surface the outbox actually uses.
package handlers

import (
	"context"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"

	dbgen "github.com/calionauta/gogogo/internal/db/sqlcgen"
)

// doWithHeader is do() plus one request header: the outbox stamps
// Idempotency-Key on the first attempt and reuses it on every retry.
func (f *handlersFixture) doWithHeader(
	target string, cookie *http.Cookie, value string,
) *httptest.ResponseRecorder {
	f.t.Helper()
	req := httptest.NewRequestWithContext(context.Background(), http.MethodPost, target, strings.NewReader(""))
	if cookie != nil {
		req.AddCookie(cookie)
	}
	if value != "" {
		req.Header.Set("Idempotency-Key", value)
	}
	rec := httptest.NewRecorder()
	f.r.ServeHTTP(rec, req)
	return rec
}

// newTodo creates one row and returns its id.
func (f *handlersFixture) newTodo(t *testing.T, cookie *http.Cookie, title string) string {
	t.Helper()
	if rec := f.do(http.MethodPost, "/api/todos", "title="+title+"&filter=all", cookie); rec.Code != http.StatusOK {
		t.Fatalf("create %q: status = %d (body: %s)", title, rec.Code, rec.Body.String())
	}
	owner := f.ownerOf(t, cookie)
	list, err := f.q.ListTodosByOwner(context.Background(), owner)
	if err != nil || len(list) == 0 {
		t.Fatalf("list after create: %d rows, %v", len(list), err)
	}
	return list[0].ID
}

func (f *handlersFixture) completedOf(t *testing.T, cookie *http.Cookie, id string) bool {
	t.Helper()
	owner := f.ownerOf(t, cookie)
	row, err := f.q.GetTodo(context.Background(), dbgen.GetTodoParams{ID: id, Owner: owner})
	if err != nil {
		t.Fatalf("read todo: %v", err)
	}
	return row.Completed
}

// TestToggleReplayIsIdempotent is the contract the outbox depends on: the same
// intent delivered twice produces ONE effect.
func TestToggleReplayIsIdempotent(t *testing.T) {
	f := newHandlersFixture(t)
	cookie := f.signup("replay@example.com")
	id := f.newTodo(t, cookie, "Replayed")

	first := f.doWithHeader("/api/todos/"+id+"/toggle?filter=all", cookie,
		"op-7f3b")
	if first.Code != http.StatusOK {
		t.Fatalf("first toggle: status = %d (body: %s)", first.Code, first.Body.String())
	}
	if !f.completedOf(t, cookie, id) {
		t.Fatal("first toggle did not complete the row")
	}

	// The response was lost; the outbox replays the same intent.
	replay := f.doWithHeader("/api/todos/"+id+"/toggle?filter=all", cookie,
		"op-7f3b")
	if replay.Code != http.StatusOK {
		t.Fatalf("replay: status = %d (body: %s)", replay.Code, replay.Body.String())
	}
	if !f.completedOf(t, cookie, id) {
		t.Fatal("the replay flipped the row back: two deliveries of one intent must be one effect")
	}
	// A replay carries no UI patch: the live stream is what tells the tab.
	if strings.Contains(replay.Body.String(), "datastar-patch-elements") {
		t.Error("a replayed mutation must not stream a DOM patch")
	}
}

// TestToggleNewKeyTogglesAgain proves the receipt is per intent, not a lock on
// the row: a genuinely new click must still toggle.
func TestToggleNewKeyTogglesAgain(t *testing.T) {
	f := newHandlersFixture(t)
	cookie := f.signup("twice@example.com")
	id := f.newTodo(t, cookie, "Twice")

	for i, key := range []string{"op-a", "op-b"} {
		rec := f.doWithHeader("/api/todos/"+id+"/toggle?filter=all", cookie,
			key)
		if rec.Code != http.StatusOK {
			t.Fatalf("toggle %d (%s): status = %d", i, key, rec.Code)
		}
	}
	if f.completedOf(t, cookie, id) {
		t.Fatal("two distinct intents toggled twice and must return to the original state")
	}
}

// TestToggleWithoutKeyStillToggles pins the compatibility shape: a caller that
// sends no key (the legacy path, a script, curl) keeps the plain read-then-flip
// semantics. The receipt is an opt-in contract, not a silent behaviour change.
func TestToggleWithoutKeyStillToggles(t *testing.T) {
	f := newHandlersFixture(t)
	cookie := f.signup("nokey@example.com")
	id := f.newTodo(t, cookie, "No key")

	for i := range 2 {
		if rec := f.do(http.MethodPost, "/api/todos/"+id+"/toggle?filter=all", "", cookie); rec.Code != http.StatusOK {
			t.Fatalf("toggle %d: status = %d", i, rec.Code)
		}
	}
	if f.completedOf(t, cookie, id) {
		t.Fatal("two keyless toggles must still flip twice")
	}
}

// TestLongKeyIsHashedNotTruncated pins the over-long-key policy. Two distinct
// intents whose first 128 bytes agree must NOT share a receipt: truncation
// would make the second toggle vanish, which is a lost write that looks like a
// working request.
func TestLongKeyIsHashedNotTruncated(t *testing.T) {
	f := newHandlersFixture(t)
	cookie := f.signup("longkey@example.com")
	id := f.newTodo(t, cookie, "Long key")

	prefix := strings.Repeat("k", 200)
	for i, key := range []string{prefix + "-one", prefix + "-two"} {
		if rec := f.doWithHeader("/api/todos/"+id+"/toggle?filter=all", cookie, key); rec.Code != http.StatusOK {
			t.Fatalf("toggle %d: status = %d", i, rec.Code)
		}
	}
	if f.completedOf(t, cookie, id) {
		t.Fatal("two distinct long intents toggled once: the keys collided after truncation")
	}
}

// TestReceiptIsOwnerScoped proves a key cannot be reused across accounts: the
// receipt is keyed by (owner, key), so one user's replay can never suppress
// another user's mutation — and an attacker cannot probe a key.
func TestReceiptIsOwnerScoped(t *testing.T) {
	f := newHandlersFixture(t)
	a := f.signup("owner-a@example.com")
	b := f.signup("owner-b@example.com")
	idA := f.newTodo(t, a, "A row")
	idB := f.newTodo(t, b, "B row")

	if rec := f.doWithHeader("/api/todos/"+idA+"/toggle?filter=all", a,
		"shared-key"); rec.Code != http.StatusOK {
		t.Fatalf("A toggle: status = %d", rec.Code)
	}
	if rec := f.doWithHeader("/api/todos/"+idB+"/toggle?filter=all", b,
		"shared-key"); rec.Code != http.StatusOK {
		t.Fatalf("B toggle: status = %d", rec.Code)
	}
	if !f.completedOf(t, b, idB) {
		t.Fatal("B's mutation was suppressed by A's receipt: the key is not owner-scoped")
	}
}
