// SCOPE:core — collaborative presence wiring (seam construction + cross-instance)
//
// One seam for BOTH collaborative features (whiteboard and notes). Split out of
// chi.go for the file-sizes gate: the seam plus its two cross-instance wires is
// a self-contained subject, and two rosters disagreeing about the same tab is
// the failure mode this shape exists to prevent.
package server

import (
	"context"
	"log/slog"

	natsio "github.com/nats-io/nats.go"

	"github.com/calionauta/gogogo/internal/collab"
	"github.com/calionauta/gogogo/internal/instance"
	"github.com/calionauta/gogogo/internal/queue"
)

// bootPresence returns the presence seam both collaborative features share: a
// federated wrapper over the in-process PeerSet. Federation starts off; it is
// switched on by AttachPresenceFederation once NATS exists.
//
// One owner per document, for both features: two rosters disagreeing about the
// same tab is the bug this shape removes.
//
// The GoAkt grain is an UPGRADE, not a parameter (see upgradePresenceToActor):
// presence must exist and work when the actor system is trimmed away, so the
// default here cannot depend on it.
func bootPresence(hub *queue.SSEHub) *collab.FederatedPresence {
	return collab.NewFederatedPresence(collab.NewPeerPresence(), hub)
}

// upgradePresenceToActor swaps the in-process roster for a scalability upgrade
// (the per-document GoAkt grain, when the actor system exists). Separate from
// bootPresence so trimming GoAkt leaves a working local presence rather than a
// nil seam: the grain is the scalable path, not the only path.
//
// It takes a PresenceSeam rather than an *appgoakt.Host so this file keeps no
// GoAkt dependency: the caller builds the grain and the trim manifest can drop
// that one call line without also deleting the helper.
func upgradePresenceToActor(p *collab.FederatedPresence, local collab.PresenceSeam) {
	if p == nil {
		return
	}
	p.UseLocal(local)
	slog.Info("server: collaborative presence upgraded", "backend", "actor", "features", "whiteboard,notes")
}

// AttachPresenceFederation makes the "X online" roster cross-instance: each
// instance publishes its own connections and merges the peers' rosters, so a
// count computed anywhere is the union. Without it the doc bridge delivered
// every join/leave frame (tabs stayed in sync) but every count still said "1
// online" while two browsers shared the document.
//
// Wraps whatever local seam the handlers hold — the GoAkt grain when the actor
// system runs, the in-process PeerSet otherwise — so a trimmed or
// GOAKT_ENABLED=false build federates exactly the same way.
//
// Nil nc is a no-op (single-instance). Call once, after Boot and after
// AttachDocBridge.
func (s *ChiStack) AttachPresenceFederation(ctx context.Context, nc *natsio.Conn) error {
	if nc == nil || s.presence == nil {
		return nil
	}
	if err := s.presence.Attach(nc, instance.ID()); err != nil {
		return err
	}
	// Same shape as startCrudConsumer: the goroutine's only exit is ctx, and a
	// non-clean stop is logged rather than swallowed. Run returns nothing, so
	// the lease simply stops holding the roster after shutdown.
	go func() {
		s.presence.Run(ctx)
		slog.Info("server: presence federation stopped")
	}()
	slog.Info("server: presence federated", "instance", instance.ID())
	return nil
}
