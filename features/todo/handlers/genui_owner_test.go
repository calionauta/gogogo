// SCOPE:feature — the todo panel's prompt digest, against a REAL database.
//
// WHY THESE TESTS EXIST. features/genui/components only ever tested DataContext's
// NEGATIVE cases (no summarizer, empty owner, truncation) — so nothing proved
// the positive path: that a row written to the Turso store actually reaches the
// model's prompt. Without that, "the AI sees your data" is a claim with no
// evidence, and the failure mode is silent: the digest comes back empty and the
// model falls back to inventing, which is the exact bug this seam was added to
// fix.
package handlers

import (
	"context"
	"strings"
	"testing"

	"github.com/calionauta/gogogo/features/todo"
	dbgen "github.com/calionauta/gogogo/internal/db/sqlcgen"
	"github.com/calionauta/gogogo/internal/dbschema"
)

// insertTodoFor writes one row for the digest tests.
//
// Local rather than reusing createStamped from search_test.go: that file is
// REMOVED by a `trim search`, so depending on it would make this file fail to
// compile in a trimmed build — which the installer's preset test catches. A
// helper a test actually needs belongs in a file that survives the trim it is
// meant to run under.
func insertTodoFor(t *testing.T, f *handlersFixture, owner, title string) {
	t.Helper()
	now := dbschema.Now()
	if _, err := f.q.CreateTodo(context.Background(), dbgen.CreateTodoParams{
		ID: dbschema.NewID(), Owner: owner, Title: title,
		Visibility: todo.VisibilityPrivate, Created: now, Updated: now,
	}); err != nil {
		t.Fatalf("create %q: %v", title, err)
	}
}

// TestTodoPanelSummaryReadsTheRealStore is the positive-path proof: a row
// created through the normal write path shows up in the prompt digest.
func TestTodoPanelSummaryReadsTheRealStore(t *testing.T) {
	t.Parallel()
	f := newHandlersFixture(t)
	cookie := f.signup("summary@example.com")
	me := f.ownerOf(t, cookie)

	insertTodoFor(t, f, me, "deploy the kestrel parser")
	insertTodoFor(t, f, me, "write the handoff note")

	panel := NewTodoPanel(f.q)
	got, err := panel.Summary(context.Background(), me)
	if err != nil {
		t.Fatalf("Summary: %v", err)
	}
	if !strings.Contains(got, "kestrel parser") || !strings.Contains(got, "handoff note") {
		t.Fatalf("the digest does not carry the stored titles: %q", got)
	}
	if !strings.Contains(got, "2 todo(s) total") {
		t.Errorf("digest = %q, want it to state the total", got)
	}
	if !strings.Contains(got, "2 open, 0 done") {
		t.Errorf("digest = %q, want the open/done split", got)
	}
}

// TestTodoPanelSummarySeparatesOpenFromDone pins the split that makes the digest
// useful for planning: "what is left" is what a plan is built from, so the
// open rows must be listed under Open and not mixed into Done.
func TestTodoPanelSummarySeparatesOpenFromDone(t *testing.T) {
	t.Parallel()
	f := newHandlersFixture(t)
	cookie := f.signup("split@example.com")
	me := f.ownerOf(t, cookie)

	insertTodoFor(t, f, me, "still to do")
	insertTodoFor(t, f, me, "already finished")
	// Mark the second one done through the store, so the count is real.
	rows, err := f.q.ListTodosByOwner(context.Background(), me)
	if err != nil {
		t.Fatalf("list: %v", err)
	}
	var doneID string
	for _, r := range rows {
		if r.Title == "already finished" {
			doneID = r.ID
		}
	}
	if doneID == "" {
		t.Fatal("fixture lost the row it just created")
	}
	if _, uerr := f.sqlDB.ExecContext(context.Background(),
		`UPDATE todos SET completed = 1 WHERE id = ?`, doneID); uerr != nil {
		t.Fatalf("complete row: %v", uerr)
	}

	got, err := NewTodoPanel(f.q).Summary(context.Background(), me)
	if err != nil {
		t.Fatalf("Summary: %v", err)
	}
	if !strings.Contains(got, "1 open, 1 done") {
		t.Errorf("digest = %q, want 1 open / 1 done", got)
	}
	openIdx := strings.Index(got, "Open:")
	doneIdx := strings.Index(got, "Done:")
	if openIdx < 0 || doneIdx < 0 {
		t.Fatalf("digest = %q, want both Open: and Done: sections", got)
	}
	openPart := got[openIdx:doneIdx]
	if !strings.Contains(openPart, "still to do") {
		t.Errorf("the open row is not under Open: %q", got)
	}
	if strings.Contains(openPart, "already finished") {
		t.Errorf("a completed row leaked into Open: %q", got)
	}
}

// TestTodoPanelSummaryIsScopedToTheOwner is the tenancy proof: a digest built
// for one user must never carry another user's rows. This is the assertion that
// matters most — the digest goes straight into a prompt, so a scoping mistake
// here is a data leak between accounts, not a cosmetic bug.
func TestTodoPanelSummaryIsScopedToTheOwner(t *testing.T) {
	t.Parallel()
	f := newHandlersFixture(t)
	aliceCookie := f.signup("alice-summary@example.com")
	bobCookie := f.signup("bob-summary@example.com")
	alice := f.ownerOf(t, aliceCookie)
	bob := f.ownerOf(t, bobCookie)

	insertTodoFor(t, f, alice, "alice private task")
	insertTodoFor(t, f, bob, "bob private task")

	got, err := NewTodoPanel(f.q).Summary(context.Background(), alice)
	if err != nil {
		t.Fatalf("Summary: %v", err)
	}
	if strings.Contains(got, "bob private task") {
		t.Fatalf("alice's digest carries bob's row: %q", got)
	}
	if !strings.Contains(got, "alice private task") {
		t.Errorf("alice's digest lost her own row: %q", got)
	}
}

// TestTodoPanelSummaryEmptyStoreSaysNothing pins the omission: an empty list
// returns "", so the caller can leave the whole block out instead of handing the
// model a negative fact it might report as an answer to another question.
func TestTodoPanelSummaryEmptyStoreSaysNothing(t *testing.T) {
	t.Parallel()
	f := newHandlersFixture(t)
	cookie := f.signup("empty-summary@example.com")
	me := f.ownerOf(t, cookie)

	got, err := NewTodoPanel(f.q).Summary(context.Background(), me)
	if err != nil {
		t.Fatalf("Summary: %v", err)
	}
	if got != "" {
		t.Errorf("empty store produced %q, want no digest at all", got)
	}
}
