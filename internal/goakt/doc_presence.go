// SCOPE:plugin — GoAkt document-presence actor (whiteboard/notes roster).
//
// One grain per document holds the client ids watching it. Join/Leave/List
// are Ask (the HTTP handler needs the roster to answer immediately), so the
// current PeerSet contract survives: a join returns the peers already there,
// a leave returns the remaining set. Cursor/typing stay on the SSE relay and
// never enter this mailbox, so a cursor flood cannot grow it.
package goakt

import (
	"context"
	"errors"
	"fmt"
	"sort"

	goakt "github.com/tochemey/goakt/v4/actor"
	"github.com/tochemey/goakt/v4/passivation"
)

// DocSnapshot is the actor's reply: the peers plus the generation (bumped on
// every restart, so a test/demo can prove the supervisor actually restarted).
type DocSnapshot struct {
	Peers      []string
	Generation int
}

type (
	docJoin  struct{ ClientID string }
	docLeave struct{ ClientID string }
	docList  struct{}
	docCrash struct{}
)

// docActor is one document's presence: a set of connection ids, no names and
// no document data. The document itself remains in Loro/Turso; this state is
// soft and rebuilds from reconnects after a restart.
type docActor struct {
	peers      map[string]struct{}
	generation int
}

func (d *docActor) PreStart(*goakt.Context) error {
	d.peers = make(map[string]struct{})
	d.generation++
	return nil
}

func (d *docActor) PostStop(*goakt.Context) error { return nil }

func (d *docActor) snapshot() DocSnapshot {
	out := DocSnapshot{Generation: d.generation}
	for id := range d.peers {
		out.Peers = append(out.Peers, id)
	}
	sort.Strings(out.Peers)
	return out
}

func (d *docActor) Receive(ctx *goakt.ReceiveContext) {
	switch msg := ctx.Message().(type) {
	case docJoin:
		d.peers[msg.ClientID] = struct{}{}
		others := make([]string, 0, len(d.peers))
		for id := range d.peers {
			if id != msg.ClientID {
				others = append(others, id)
			}
		}
		sort.Strings(others)
		ctx.Response(others)
	case docLeave:
		delete(d.peers, msg.ClientID)
		ctx.Response(d.snapshot())
	case docList:
		ctx.Response(d.snapshot())
	case docCrash:
		panic("doc presence crash requested (demo/test hook)")
	}
}

// DocPresence is the GoAkt-backed Presence implementation.
type DocPresence struct{ host *Host }

// NewDocPresence wraps a started host. A nil host is a wiring error; the
// composition root only calls this when bootRoomActor returned a host.
func NewDocPresence(host *Host) *DocPresence { return &DocPresence{host: host} }

func (p *DocPresence) doc(ctx context.Context, id string) (*goakt.PID, error) {
	id = sanitizeRoomID(id)
	h := p.host
	h.mu.Lock()
	defer h.mu.Unlock()
	key := "doc-" + id
	if pid, ok := h.rooms[key]; ok {
		return pid, nil
	}
	if h.system == nil {
		return nil, errors.New("goakt: host not started")
	}
	pid, err := h.system.Spawn(ctx, key, &docActor{},
		goakt.WithSupervisor(h.sup),
		goakt.WithPassivationStrategy(passivation.NewTimeBasedStrategy(IdlePassivateAfter)),
	)
	if err != nil {
		return nil, fmt.Errorf("goakt: spawn doc presence %q: %w", id, err)
	}
	h.rooms[key] = pid
	return pid, nil
}

func (p *DocPresence) ask(ctx context.Context, id string, msg any) (any, error) {
	pid, err := p.doc(ctx, id)
	if err != nil {
		return nil, err
	}
	return goakt.Ask(ctx, pid, msg, AskTimeout)
}

// Join registers one connection and returns the other connections already
// present (the same shape PeerSet.Join returns).
func (p *DocPresence) Join(ctx context.Context, docID, clientID string) ([]string, error) {
	raw, err := p.ask(ctx, docID, docJoin{ClientID: clientID})
	if err != nil {
		return nil, fmt.Errorf("goakt: doc join %q: %w", docID, err)
	}
	out, ok := raw.([]string)
	if !ok {
		return nil, fmt.Errorf("goakt: doc join %q: unexpected reply %T", docID, raw)
	}
	return out, nil
}

// Leave removes one connection and returns the remaining peers.
func (p *DocPresence) Leave(ctx context.Context, docID, clientID string) ([]string, error) {
	raw, err := p.ask(ctx, docID, docLeave{ClientID: clientID})
	if err != nil {
		return nil, fmt.Errorf("goakt: doc leave %q: %w", docID, err)
	}
	out, ok := raw.(DocSnapshot)
	if !ok {
		return nil, fmt.Errorf("goakt: doc leave %q: unexpected reply %T", docID, raw)
	}
	return out.Peers, nil
}

// List returns the peers currently on the document.
func (p *DocPresence) List(ctx context.Context, docID string) ([]string, error) {
	raw, err := p.ask(ctx, docID, docList{})
	if err != nil {
		return nil, fmt.Errorf("goakt: doc list %q: %w", docID, err)
	}
	out, ok := raw.(DocSnapshot)
	if !ok {
		return nil, fmt.Errorf("goakt: doc list %q: unexpected reply %T", docID, raw)
	}
	return out.Peers, nil
}

// Crash is the test/demo hook: the supervisor restarts the grain and the
// generation bumps. The roster is soft state; clients rebuild it on reconnect.
func (p *DocPresence) Crash(ctx context.Context, docID string) error {
	pid, err := p.doc(ctx, docID)
	if err != nil {
		return err
	}
	return goakt.Tell(ctx, pid, docCrash{})
}

// Snapshot exposes the generation for tests and a future actor-inspector UI.
func (p *DocPresence) Snapshot(ctx context.Context, docID string) (DocSnapshot, error) {
	raw, err := p.ask(ctx, docID, docList{})
	if err != nil {
		return DocSnapshot{}, err
	}
	out, ok := raw.(DocSnapshot)
	if !ok {
		return DocSnapshot{}, fmt.Errorf("goakt: doc snapshot %q: unexpected reply %T", docID, raw)
	}
	return out, nil
}
