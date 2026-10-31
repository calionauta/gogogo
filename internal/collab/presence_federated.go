// SCOPE:plugin — Federated document presence (local roster + peer instances).
//
// The hub's doc bridge carries presence FRAMES between instances, which is
// enough for "I saw a peer join". It is NOT enough for the authoritative
// "X online" count: each instance only knows its own connections, so a count
// computed from the local roster under-reports the moment two tabs land on
// different pods. Two tabs on one board read "1 online" while both were
// drawing on it — and the pill is the only signal a user has that the board is
// shared at all.
//
// FederatedPresence closes that: every instance publishes its OWN local roster
// for a document on app.roster.<docID>, and every instance merges what it
// receives. List returns the union, so the count frame is honest across
// instances.
//
// Convergence, not consensus: snapshots are idempotent full sets (never
// deltas), so a dropped message is healed by the next publish. Liveness is a
// lease — each instance re-publishes its roster on a ticker, and a remote entry
// expires when it stops being refreshed (a SIGKILLed pod cannot announce its own
// death). An orderly shutdown publishes an empty roster so peers drop those
// connections immediately instead of waiting out the lease.
package collab

import (
	"context"
	"encoding/json"
	"errors"
	"log/slog"
	"sync"
	"time"

	natsio "github.com/nats-io/nats.go"

	"github.com/calionauta/gogogo/internal/observability"
	"github.com/calionauta/gogogo/internal/queue"
)

// RosterSubjectPrefix carries one instance's local roster for one document.
// Its own family — not app.docbridge.* (UI frames) and not app.sync.* (CRDT
// bytes): a roster SNAPSHOT is neither, and mixing them would either duplicate
// deliveries or make the hub parse roster envelopes as tab payloads.
const RosterSubjectPrefix = "app.roster."

// RosterSubject returns the publish subject for one document's rosters.
func RosterSubject(docID string) string { return RosterSubjectPrefix + docID }

// RosterSubscribe is the wildcard every federated instance subscribes to.
const RosterSubscribe = RosterSubjectPrefix + ">"

const (
	// rosterLease is how often an instance re-publishes its local roster. It
	// bounds how long peer tabs can show a connection from a pod that died
	// without announcing it.
	rosterLease = 10 * time.Second
	// rosterTTL is when a remote entry is dropped for lack of a refresh: three
	// missed leases, so one lost publish does not flap the count.
	rosterTTL = 3 * rosterLease
)

// rosterSnapshot is one instance's full local roster for one document.
type rosterSnapshot struct {
	Instance string   `json:"instance"`
	Peers    []string `json:"peers"`
	TS       int64    `json:"ts"`
}

type remoteRoster struct {
	peers []string
	seen  time.Time
}

// FederatedPresence implements PresenceSeam by delegating to the instance-local
// backend (PeerSet or the GoAkt grain) and merging the rosters other instances
// publish. Until Attach runs it IS that local seam, so a single-instance or
// NATS-off boot behaves exactly as before.
type FederatedPresence struct {
	local PresenceSeam
	hub   *queue.SSEHub

	mu        sync.Mutex
	nc        *natsio.Conn
	instance  string
	remote    map[string]map[string]remoteRoster // docID → instanceID → roster
	published map[string]struct{}                // docs this instance announced
}

// NewFederatedPresence wraps a local seam and the hub that receives the merged
// count broadcasts. It is NOT federated yet: call Attach once a NATS connection
// exists (the composition root boots before the engine owns NATS).
func NewFederatedPresence(local PresenceSeam, hub *queue.SSEHub) *FederatedPresence {
	return &FederatedPresence{
		local:     local,
		hub:       hub,
		remote:    make(map[string]map[string]remoteRoster),
		published: make(map[string]struct{}),
	}
}

// Attach turns federation on: this instance subscribes to peer rosters and
// publishes its own. instanceID identifies the process so its own publishes are
// ignored on arrival.
//
// Returns an error only when the subscription cannot be established: a caller
// that HAS a NATS connection but cannot federate should fail loudly at boot
// rather than silently under-report presence.
func (f *FederatedPresence) Attach(nc *natsio.Conn, instanceID string) error {
	if nc == nil {
		return nil
	}
	f.mu.Lock()
	f.nc = nc
	f.instance = instanceID
	f.mu.Unlock()
	if _, err := nc.Subscribe(RosterSubscribe, f.onSnapshot); err != nil {
		return err
	}
	// Flush the SUB before returning: a join immediately after attach can
	// otherwise race interest registration and go unheard (same reason the
	// hub's bridges flush).
	return nc.Flush()
}

// localSeam reads the current local backend under the lock: UseLocal may swap
// it (actor upgrade) while requests are in flight.
func (f *FederatedPresence) localSeam() PresenceSeam {
	f.mu.Lock()
	defer f.mu.Unlock()
	return f.local
}

// UseLocal swaps the instance-local backend, keeping every remote entry and
// subscription intact: the GoAkt grain is an upgrade of WHERE the local roster
// lives, not a restart of federation. Safe before or after Attach.
//
// Nil is ignored: a trimmed/disabled actor system leaves the PeerSet fallback,
// so presence keeps working when the engine is absent.
func (f *FederatedPresence) UseLocal(local PresenceSeam) {
	if local == nil {
		return
	}
	f.mu.Lock()
	defer f.mu.Unlock()
	f.local = local
}

// Run holds the lease open until ctx ends, then retracts every roster this
// instance published. Callers run it in a goroutine under the process lifetime;
// cancelling ctx is the shutdown path.
func (f *FederatedPresence) Run(ctx context.Context) {
	f.mu.Lock()
	active := f.nc != nil
	f.mu.Unlock()
	if !active {
		<-ctx.Done()
		return
	}
	tick := time.NewTicker(rosterLease)
	defer tick.Stop()
	for {
		select {
		case <-ctx.Done():
			f.retractAll()
			return
		case <-tick.C:
			f.renewLease(ctx)
		}
	}
}

// Join registers a connection locally and returns the MERGED roster.
func (f *FederatedPresence) Join(ctx context.Context, docID, clientID string) ([]string, error) {
	if _, err := f.localSeam().Join(ctx, docID, clientID); err != nil {
		return nil, err
	}
	f.announce(ctx, docID)
	return f.List(ctx, docID)
}

// Leave removes a connection locally and returns the MERGED remaining roster.
func (f *FederatedPresence) Leave(ctx context.Context, docID, clientID string) ([]string, error) {
	if _, err := f.localSeam().Leave(ctx, docID, clientID); err != nil {
		return nil, err
	}
	f.announce(ctx, docID)
	return f.List(ctx, docID)
}

// List returns the union of this instance's roster and every live peer roster.
func (f *FederatedPresence) List(ctx context.Context, docID string) ([]string, error) {
	local, err := f.localSeam().List(ctx, docID)
	if err != nil {
		return nil, err
	}
	f.mu.Lock()
	defer f.mu.Unlock()
	seen := make(map[string]struct{}, len(local))
	out := make([]string, 0, len(local))
	add := func(ids []string) {
		for _, id := range ids {
			if _, dup := seen[id]; dup {
				continue
			}
			seen[id] = struct{}{}
			out = append(out, id)
		}
	}
	add(local)
	for _, r := range f.remote[docID] {
		add(r.peers)
	}
	return out, nil
}

// ForgetDoc drops the document everywhere: locally, from this instance's
// published set, and from the merged view. A peer instance retracts its own
// entry when its tabs leave.
func (f *FederatedPresence) ForgetDoc(ctx context.Context, docID string) error {
	if err := f.localSeam().ForgetDoc(ctx, docID); err != nil {
		return err
	}
	f.mu.Lock()
	delete(f.published, docID)
	delete(f.remote, docID)
	f.mu.Unlock()
	return nil
}

// announce publishes this instance's local roster for docID (no-op until
// Attach). Called on every local join/leave, so peers learn about it at once
// instead of waiting for the next lease tick.
func (f *FederatedPresence) announce(ctx context.Context, docID string) {
	f.mu.Lock()
	publish := f.nc != nil
	f.mu.Unlock()
	if !publish {
		return
	}
	peers, err := f.localSeam().List(ctx, docID)
	if err != nil {
		obsArgs := append(observability.WithComponent("collab", "plugin"), "doc", docID, "error", err)
		observability.Warn(ctx, "collab: federated roster list failed", obsArgs...)
		return
	}
	f.publish(docID, peers)
}

// publish puts one roster snapshot on the wire and records the doc as
// announced, so the lease keeps it alive.
func (f *FederatedPresence) publish(docID string, peers []string) {
	f.mu.Lock()
	nc, instance := f.nc, f.instance
	if nc != nil {
		f.published[docID] = struct{}{}
	}
	f.mu.Unlock()
	if nc == nil {
		return
	}
	if peers == nil {
		// JSON `null` would decode to a nil slice; an explicit empty list keeps
		// "this instance has nobody here" distinct from "no field".
		peers = []string{}
	}
	payload, err := json.Marshal(rosterSnapshot{
		Instance: instance, Peers: peers, TS: time.Now().UnixMilli(),
	})
	if err != nil {
		return
	}
	if pubErr := nc.Publish(RosterSubject(docID), payload); pubErr != nil {
		// Expected during shutdown: the lease ticker or an in-flight leave can
		// outlive the connection by one tick. Anything but a closed connection is
		// a real publish failure worth surfacing.
		if !errors.Is(pubErr, natsio.ErrConnectionClosed) {
			slog.Warn("collab: roster publish failed", "doc", docID, "error", pubErr)
		}
	}
}

// retractAll announces empty rosters so peers drop this instance's connections
// now instead of waiting out the lease.
func (f *FederatedPresence) retractAll() {
	f.mu.Lock()
	docs := make([]string, 0, len(f.published))
	for doc := range f.published {
		docs = append(docs, doc)
	}
	f.published = make(map[string]struct{})
	f.mu.Unlock()
	for _, doc := range docs {
		f.publish(doc, []string{})
	}
}

// renewLease refreshes every announced roster and drops expired remote entries.
// The lease tick inherits the Run lifetime, so a shutdown cancels the local
// roster reads instead of leaving an Ask in flight against a stopping host.
func (f *FederatedPresence) renewLease(ctx context.Context) {
	f.mu.Lock()
	docs := make([]string, 0, len(f.published))
	for doc := range f.published {
		docs = append(docs, doc)
	}
	f.mu.Unlock()

	for _, doc := range docs {
		peers, err := f.localSeam().List(ctx, doc)
		if err != nil {
			continue
		}
		// Stop renewing a doc this instance no longer serves locally, unless a
		// peer still does (the union is still worth publishing).
		if len(peers) == 0 && f.remoteCount(doc) == 0 {
			f.mu.Lock()
			delete(f.published, doc)
			f.mu.Unlock()
			continue
		}
		f.publish(doc, peers)
	}
	f.expireRemote()
}

// expireRemote forgets rosters whose instance stopped refreshing them.
func (f *FederatedPresence) expireRemote() {
	f.mu.Lock()
	defer f.mu.Unlock()
	now := time.Now()
	for doc, byInstance := range f.remote {
		for inst, r := range byInstance {
			if now.Sub(r.seen) > rosterTTL {
				delete(byInstance, inst)
			}
		}
		if len(byInstance) == 0 {
			delete(f.remote, doc)
		}
	}
}

// remoteCount reports how many peer instances currently report docID.
func (f *FederatedPresence) remoteCount(docID string) int {
	f.mu.Lock()
	defer f.mu.Unlock()
	return len(f.remote[docID])
}

// onSnapshot records one peer instance's roster and re-broadcasts the merged
// count, so a change originating on another instance reaches the tabs here.
func (f *FederatedPresence) onSnapshot(m *natsio.Msg) {
	docID, ok := docIDFromRosterSubject(m.Subject)
	if !ok {
		return
	}
	var snap rosterSnapshot
	if err := json.Unmarshal(m.Data, &snap); err != nil {
		slog.Warn("collab: roster snapshot decode", "subject", m.Subject, "error", err)
		return
	}
	f.mu.Lock()
	own := snap.Instance == "" || snap.Instance == f.instance
	f.mu.Unlock()
	if own {
		// Our own echo: this instance already holds its local roster.
		return
	}
	if !f.recordRemote(docID, snap) {
		return
	}
	// The merged count is what the tabs render, so re-broadcast it on every
	// peer change — locally and, via the doc bridge, to the other instances'
	// tabs (which merge the same snapshots and reach the same union).
	f.broadcastCount(docID)
}

// recordRemote stores a peer roster, reporting whether the merged view changed.
func (f *FederatedPresence) recordRemote(docID string, snap rosterSnapshot) bool {
	f.mu.Lock()
	defer f.mu.Unlock()
	byInstance := f.remote[docID]
	if byInstance == nil {
		byInstance = make(map[string]remoteRoster)
		f.remote[docID] = byInstance
	}
	prev, existed := byInstance[snap.Instance]
	changed := !existed || !equalPeers(prev.peers, snap.Peers)
	byInstance[snap.Instance] = remoteRoster{peers: snap.Peers, seen: time.Now()}
	return changed
}

// broadcastCount sends the merged roster as the authoritative count frame.
func (f *FederatedPresence) broadcastCount(docID string) {
	if f.hub == nil {
		return
	}
	peers, err := f.List(context.Background(), docID)
	if err != nil {
		return
	}
	payload, mErr := json.Marshal(PresenceMsg{Doc: docID, Type: "count", Peers: peers})
	if mErr != nil {
		return
	}
	f.hub.BroadcastToDoc(payload, docID)
}

// docIDFromRosterSubject extracts the doc id from "app.roster.<docID>". Doc ids
// are single NATS tokens at this layer (the same assumption app.sync.<doc>
// makes), so a dotted id is rejected rather than silently truncated.
func docIDFromRosterSubject(subject string) (string, bool) {
	const n = len(RosterSubjectPrefix)
	if len(subject) <= n || subject[:n] != RosterSubjectPrefix {
		return "", false
	}
	docID := subject[n:]
	for i := range len(docID) {
		if docID[i] == '.' {
			return "", false
		}
	}
	return docID, true
}

// equalPeers compares two rosters as sets (order is not part of the contract).
func equalPeers(a, b []string) bool {
	if len(a) != len(b) {
		return false
	}
	set := make(map[string]struct{}, len(a))
	for _, id := range a {
		set[id] = struct{}{}
	}
	for _, id := range b {
		if _, ok := set[id]; !ok {
			return false
		}
	}
	return true
}
