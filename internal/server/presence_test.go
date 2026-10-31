// SCOPE:core — presence seam construction and the GoAkt upgrade it routes to.

package server

import (
	"context"
	"slices"
	"sync"
	"testing"

	"github.com/calionauta/gogogo/internal/collab"
	appgoakt "github.com/calionauta/gogogo/internal/goakt"
	"github.com/calionauta/gogogo/internal/queue"
)

// recordingSeam is a PresenceSeam that counts calls, so a test can tell WHICH
// backend a join landed on (the in-process fallback or the upgraded grain).
type recordingSeam struct {
	mu     sync.Mutex
	joined []string
}

func (r *recordingSeam) Join(_ context.Context, _ string, clientID string) ([]string, error) {
	r.mu.Lock()
	defer r.mu.Unlock()
	r.joined = append(r.joined, clientID)
	return nil, nil
}

func (r *recordingSeam) Leave(_ context.Context, _, _ string) ([]string, error) {
	return nil, nil
}

func (r *recordingSeam) List(_ context.Context, _ string) ([]string, error) {
	return nil, nil
}

func (r *recordingSeam) ForgetDoc(_ context.Context, _ string) error { return nil }

func (r *recordingSeam) saw(clientID string) bool {
	r.mu.Lock()
	defer r.mu.Unlock()
	return slices.Contains(r.joined, clientID)
}

// TestUpgradePresenceToActorRoutesJoinsToTheGrain is the guard that GoAkt
// actually backs collaborative presence, in BOTH features, rather than the
// in-process PeerSet quietly serving it.
//
// RED-PROOF: make upgradePresenceToActor a no-op (delete its UseLocal call) and
// the join lands on the fallback, so `saw` is false and this fails. Before this
// test, nothing asserted the upgrade took effect: internal/goakt's own tests
// exercise DocPresence directly, and the feature suites wire their own
// FederatedPresence, so a silently dead upgrade would leave every test green
// while production ran on the fallback.
func TestUpgradePresenceToActorRoutesJoinsToTheGrain(t *testing.T) {
	hub := queue.NewSSEHub()
	t.Cleanup(hub.Close)

	federated := bootPresence(hub)

	// The seam bootPresence installed (the fallback) must not see the join.
	// Swap a recorder in as the "grain" and prove the join reaches IT.
	grain := &recordingSeam{}
	upgradePresenceToActor(federated, grain)

	if _, err := federated.Join(context.Background(), "doc-1", "tab-from-actor"); err != nil {
		t.Fatalf("join through the upgraded seam: %v", err)
	}
	if !grain.saw("tab-from-actor") {
		t.Fatal("join did not reach the GoAkt-backed seam: the upgrade is inert, " +
			"so presence is being served by the in-process fallback")
	}
}

// TestBootPresenceIsUsableWithoutGoAkt pins the other half: the grain is an
// upgrade, not a requirement. A trimmed or GOAKT_ENABLED=false build must still
// have a working roster, or presence dies with the actor system.
//
// RED-PROOF: make bootPresence return a FederatedPresence with a nil local seam
// and the join panics/fails here.
func TestBootPresenceIsUsableWithoutGoAkt(t *testing.T) {
	hub := queue.NewSSEHub()
	t.Cleanup(hub.Close)

	federated := bootPresence(hub) // no upgrade: the fallback path
	ctx := context.Background()

	if _, err := federated.Join(ctx, "doc-1", "tab-a"); err != nil {
		t.Fatalf("join on the fallback seam: %v", err)
	}
	peers, err := federated.List(ctx, "doc-1")
	if err != nil {
		t.Fatalf("list on the fallback seam: %v", err)
	}
	if len(peers) != 1 || peers[0] != "tab-a" {
		t.Fatalf("fallback roster = %v, want [tab-a]", peers)
	}
}

// TestRealGoAktGrainServesPresence runs the actual actor system through the
// same path production takes (bootPresence + upgradePresenceToActor) instead of
// a recorder. It is what makes "GoAkt backs presence" a verified claim rather
// than a wiring assumption: the recorder above proves the upgrade is not
// inert, and this proves the grain it upgrades TO satisfies the roster contract
// with doc scoping intact.
func TestRealGoAktGrainServesPresence(t *testing.T) {
	hub := queue.NewSSEHub()
	t.Cleanup(hub.Close)
	ctx := context.Background()

	host := appgoakt.New()
	if err := host.Start(ctx); err != nil {
		t.Fatalf("start goakt host: %v", err)
	}
	t.Cleanup(func() { _ = host.Stop(ctx) })

	federated := bootPresence(hub)
	upgradePresenceToActor(federated, appgoakt.NewDocPresence(host))

	if _, err := federated.Join(ctx, "doc-a", "tab1"); err != nil {
		t.Fatalf("join tab1: %v", err)
	}
	if _, err := federated.Join(ctx, "doc-a", "tab2"); err != nil {
		t.Fatalf("join tab2: %v", err)
	}
	if _, err := federated.Join(ctx, "doc-b", "tabZ"); err != nil {
		t.Fatalf("join tabZ: %v", err)
	}

	got, err := federated.List(ctx, "doc-a")
	if err != nil {
		t.Fatalf("list doc-a: %v", err)
	}
	if len(got) != 2 {
		t.Fatalf("doc-a roster = %v, want 2 tabs (the grain lost a join)", got)
	}
	other, err := federated.List(ctx, "doc-b")
	if err != nil {
		t.Fatalf("list doc-b: %v", err)
	}
	if len(other) != 1 || other[0] != "tabZ" {
		t.Fatalf("doc-b roster = %v, want [tabZ] (doc scoping broke in the grain)", other)
	}
}

// The concrete GoAkt grain must implement what the composition root hands it:
// a changed method set on either side fails the build here rather than at a
// boot that silently fell back to the PeerSet. A package-level assertion, not a
// test — a function whose body is a nil-var declaration never fails at runtime,
// so wrapping it in Test() would claim a guard it does not provide.
var _ collab.PresenceSeam = (*appgoakt.DocPresence)(nil)

// TestBothFeaturesBypassPeerSetWhenGoAktRuns is the end-to-end claim, in the
// production boot shape: one agent host, bootPresence + upgradePresenceToActor
// exactly as BootChi does it, and the SAME seam handed to the whiteboard and
// notes handlers.
//
// It asserts the thing a reader actually wants to know — with GoAkt enabled,
// neither feature falls back to the in-process PeerSet — by asking each handle
// for a clientID that was never joined locally. The PeerSet answers "not on the
// roster"; the GoAkt grain answers the same, so the discriminating check is the
// REVERSE: a join made directly on the PeerSet is invisible to the seam, while
// a join through the seam is visible to the grain. If the upgrade were inert,
// those two would swap and this test fails.
func TestBothFeaturesBypassPeerSetWhenGoAktRuns(t *testing.T) {
	hub := queue.NewSSEHub()
	t.Cleanup(hub.Close)
	ctx := context.Background()

	host := appgoakt.New()
	if err := host.Start(ctx); err != nil {
		t.Fatalf("start goakt host: %v", err)
	}
	t.Cleanup(func() { _ = host.Stop(ctx) })

	// Exactly the BootChi sequence.
	federated := bootPresence(hub)
	grain := appgoakt.NewDocPresence(host)
	upgradePresenceToActor(federated, grain)

	// The seam now routes to the grain: read through the grain directly and see
	// the join the seam made.
	if _, err := federated.Join(ctx, "shared-doc", "tab-via-seam"); err != nil {
		t.Fatalf("join through the upgraded seam: %v", err)
	}
	viaGrain, err := grain.List(ctx, "shared-doc")
	if err != nil {
		t.Fatalf("grain list: %v", err)
	}
	if !slices.Contains(viaGrain, "tab-via-seam") {
		t.Fatalf("grain roster = %v, want to contain tab-via-seam: the seam is not "+
			"routing to GoAkt, so both features are on the PeerSet", viaGrain)
	}

	// And the PeerSet is NOT where it landed: a fresh PeerSet (what the feature
	// would be using if the upgrade never took effect) knows nothing about it.
	plain := collab.NewPeerPresence()
	peers, err := plain.List(ctx, "shared-doc")
	if err != nil {
		t.Fatalf("peerset list: %v", err)
	}
	if slices.Contains(peers, "tab-via-seam") {
		t.Fatalf("the join is visible in a standalone PeerSet: the upgrade did "+
			"not take effect (peers=%v)", peers)
	}
}
