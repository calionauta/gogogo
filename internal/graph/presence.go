// Presence source: one room as a Document. The domain stays
// engine-free — the room host is reached through a 1-method reader
// injected at boot, so trimming the actor never breaks this file.
package graph

import (
	"context"
	"sort"
	"time"
)

// Member is one live presence identity.
type Member struct {
	Name     string
	LastSeen time.Time
}

// Roster is the room grain reply.
type Roster struct {
	Members    []Member
	Presenter  string
	Generation int
}

// PresenceReader is the seam: *goakt.Host satisfies it in prod.
type PresenceReader interface {
	Roster(ctx context.Context, room string) (Roster, error)
}

// Presence builds the room document: room hub node + one node per
// member. Empty roster is an empty graph (never an error) — absence
// of members is a state, not a failure.
func Presence(room string, ro Roster) Document {
	if room == "" {
		room = "lobby"
	}
	doc := Document{Source: "presence"}
	members := append([]Member(nil), ro.Members...)
	sort.Slice(members, func(i, j int) bool { return members[i].Name < members[j].Name })
	doc.Nodes = append(doc.Nodes, Node{
		ID: "room:" + room, Label: room, Kind: "room",
		Status: "active", Meta: itoa(len(members)) + " members",
	})
	for _, m := range members {
		status := "active"
		meta := ""
		if m.Name == ro.Presenter && ro.Presenter != "" {
			meta = "presenter"
		}
		doc.Nodes = append(doc.Nodes, Node{
			ID: "member:" + m.Name, Label: m.Name, Kind: "member",
			Status: status, Meta: meta,
		})
		doc.Edges = append(doc.Edges, Edge{From: "room:" + room, To: "member:" + m.Name, Kind: DependsKind})
	}
	layout(&doc)
	return doc
}

// PresenceUnavailable is the degrade document when the actor is off.
func PresenceUnavailable(room string) Document {
	if room == "" {
		room = "lobby"
	}
	doc := Document{Source: "presence:unavailable"}
	doc.Nodes = append(doc.Nodes, Node{
		ID: "room:" + room, Label: room, Kind: "room", Status: "unavailable", Meta: "actor off",
	})
	layout(&doc)
	return doc
}

func itoa(n int) string {
	if n == 0 {
		return "0"
	}
	digits := []byte{}
	for n > 0 {
		digits = append([]byte{byte('0' + n%10)}, digits...)
		n /= 10
	}
	return string(digits)
}
