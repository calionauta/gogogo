package graph

import (
	"testing"
	"time"
)

func TestPresenceListsMembersWithPresenter(t *testing.T) {
	ro := Roster{
		Members: []Member{
			{Name: "bob@example.com", LastSeen: time.Now()},
			{Name: "ana@example.com", LastSeen: time.Now()},
		},
		Presenter:  "ana@example.com",
		Generation: 3,
	}
	doc := Presence("lobby", ro)
	if doc.Source != "presence" {
		t.Fatalf("source = %q, want presence", doc.Source)
	}
	if len(doc.Nodes) != 3 {
		t.Fatalf("nodes = %d, want 3 (room + 2 members): %+v", len(doc.Nodes), doc.Nodes)
	}
	if doc.Nodes[0].Kind != "room" {
		t.Fatalf("first node = %+v, want room hub", doc.Nodes[0])
	}
	if len(doc.Edges) != 2 {
		t.Fatalf("edges = %v, want 2", doc.Edges)
	}
	// Members are sorted: ana before bob.
	if doc.Nodes[1].Label != "ana@example.com" || doc.Nodes[2].Label != "bob@example.com" {
		t.Fatalf("members unsorted: %q, %q", doc.Nodes[1].Label, doc.Nodes[2].Label)
	}
	if doc.Nodes[1].Meta != "presenter" {
		t.Fatalf("presenter meta = %q", doc.Nodes[1].Meta)
	}
}

func TestPresenceEmptyHasHubOnly(t *testing.T) {
	doc := Presence("", Roster{})
	if doc.Source != "presence" {
		t.Fatalf("source = %q", doc.Source)
	}
	if len(doc.Nodes) != 1 || doc.Nodes[0].ID != "room:lobby" {
		t.Fatalf("nodes = %+v, want lobby hub alone", doc.Nodes)
	}
	if doc.Nodes[0].Meta != "0 members" {
		t.Fatalf("meta = %q", doc.Nodes[0].Meta)
	}
}

func TestPresenceUnavailableIsNotAnError(t *testing.T) {
	doc := PresenceUnavailable("lobby")
	if doc.Source != "presence:unavailable" {
		t.Fatalf("source = %q", doc.Source)
	}
	if len(doc.Nodes) == 0 || doc.Nodes[0].Status != "unavailable" {
		t.Fatalf("node = %+v", doc.Nodes)
	}
}
