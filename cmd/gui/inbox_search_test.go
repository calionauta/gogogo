package main

import (
	"context"
	"testing"

	dbgen "github.com/calionauta/gogogo/internal/db/sqlcgen"
	"github.com/calionauta/gogogo/internal/dbschema"
)

func TestNativeInboxAndSearch(t *testing.T) {
	s := newNativeState(t)
	signInNative(t, s)

	// Search narrows the todo list in memory (same snapshot the view reads).
	s.setDraft("buy oat milk")
	s.add()
	s.setDraft("call bank")
	s.add()
	s.setSearch("oat")
	snap := s.snapshot()
	if got := visibleItems(snap.items, snap.search); len(got) != 1 || got[0].Title != "buy oat milk" {
		t.Fatalf("search filter = %v, want only oat milk", got)
	}
	if got := visibleItems(snap.items, ""); len(got) != 2 {
		t.Fatalf("empty search = %d, want identity", len(got))
	}

	// Inbox reads the same notifications table the web page does.
	owner := snap.ownerID
	_, err := s.st.q.CreateNotification(context.Background(), dbgen.CreateNotificationParams{
		ID: dbschema.NewID(), Owner: owner, Workflow: "reminder",
		TransactionID: dbschema.NewID(), Channel: "in_app", Status: "delivered",
		Subject: "Reminder", Body: "standup at 9", Created: dbschema.Now(),
	})
	if err != nil {
		t.Fatalf("create notification: %v", err)
	}
	s.refresh()
	snap = s.snapshot()
	if len(snap.notifs) != 1 || snap.unread != 1 {
		t.Fatalf("notifs = %d unread = %d, want 1/1", len(snap.notifs), snap.unread)
	}
	if !snap.notifs[0].Unread || snap.notifs[0].Subject != "Reminder" {
		t.Fatalf("notif = %+v", snap.notifs[0])
	}
}
