package main

import (
	"context"
	"testing"

	dbgen "github.com/calionauta/gogogo/internal/db/sqlcgen"
	"github.com/calionauta/gogogo/internal/dbschema"
)

func TestNativeDocsAndReviewSections(t *testing.T) {
	s := newNativeState(t)
	signInNative(t, s)
	owner := s.snapshot().ownerID

	if err := s.st.q.UpsertNote(context.Background(), dbgen.UpsertNoteParams{
		DocID: "note-1", Owner: owner, Updated: dbschema.Now(), Created: dbschema.Now(),
	}); err != nil {
		t.Fatalf("upsert note: %v", err)
	}
	if err := s.st.q.UpsertBoard(context.Background(), dbgen.UpsertBoardParams{
		DocID: "board-1", Owner: owner, Updated: dbschema.Now(), Created: dbschema.Now(),
	}); err != nil {
		t.Fatalf("upsert board: %v", err)
	}
	s.setReviewConfigured(true)
	s.refresh()
	snap := s.snapshot()
	if len(snap.notes) != 1 || snap.notes[0].ID != "note-1" {
		t.Fatalf("notes = %+v, want note-1", snap.notes)
	}
	if len(snap.boards) != 1 || snap.boards[0].ID != "board-1" {
		t.Fatalf("boards = %+v, want board-1", snap.boards)
	}
	if !snap.reviewConfigured {
		t.Fatal("reviewConfigured not propagated to snapshot")
	}
}
