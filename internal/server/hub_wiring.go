// SCOPE:core — SSE hub cross-instance wiring (ephemeral signals + doc fan-out)
//
// Two wires with disjoint traffic, both attached by cmd/web after startNATS.
// Split out of chi.go for the file-sizes gate; keeping them side by side is
// deliberate, because the only way to get this wrong is to point both at one
// subject and deliver every event twice.
package server

import (
	natsio "github.com/nats-io/nats.go"
)

// AttachDocBridge bridges the hub's DOC-SCOPED fan-out (presence join/leave/
// count, cursors, drafts, the room roster) across instances over core NATS.
// Without it every one of those was silently per-instance: a peer tab on
// another instance saw no join, the presence pill read "1 online", and the
// board looked empty of people while two browsers were drawing on it.
//
// Separate from AttachEphemeral on purpose — the two carry disjoint traffic
// (ephemeral = Send/Broadcast, doc bridge = BroadcastToDoc/Except) and a
// single subject would deliver every doc event twice. Nil (NATS off) leaves
// doc fan-out in-process, which is the documented single-instance behaviour.
// Call once, after Boot.
//
// The bridge alone carries FRAMES; the authoritative roster is federated
// separately (see AttachPresenceFederation), because a per-instance roster
// under-reports the moment two tabs land on different instances.
func (s *ChiStack) AttachDocBridge(nc *natsio.Conn) error {
	return s.hub.AttachDocBridge(nc)
}

// AttachEphemeral bridges the hub's ephemeral signals (chat answers,
// onboarding progress, suggest/retry toasts) across instances over core NATS.
// Nil (NATS off) leaves the hub in-process; record and collab fan-out keep
// their own wires. Call once, after Boot.
func (s *ChiStack) AttachEphemeral(nc *natsio.Conn) error {
	return s.hub.AttachEphemeral(nc)
}
