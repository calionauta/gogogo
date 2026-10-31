// SCOPE:plugin — GoAkt document-presence adapter tests.
package goakt

import (
	"slices"
	"testing"
	"time"
)

// TestDocPresenceMatchesPeerSet pins the seam the whiteboard consumes: join
// returns the other connections, leave returns the remaining set, two tabs of
// one person are two client ids, and documents do not share a roster.
func TestDocPresenceMatchesPeerSet(t *testing.T) {
	ctx, host := testHost(t)
	p := NewDocPresence(host)

	peers, err := p.Join(ctx, "doc-a", "c1")
	if err != nil || len(peers) != 0 {
		t.Fatalf("first join = %v, %v; want empty", peers, err)
	}
	peers, err = p.Join(ctx, "doc-a", "c2")
	if err != nil || !slices.Equal(peers, []string{"c1"}) {
		t.Fatalf("second join = %v, %v; want [c1]", peers, err)
	}
	if _, jerr := p.Join(ctx, "doc-a", "c3"); jerr != nil {
		t.Fatalf("third join: %v", jerr)
	}
	if got, lerr := p.List(ctx, "doc-a"); lerr != nil || len(got) != 3 {
		t.Fatalf("list = %v, %v; want 3", got, lerr)
	}
	remaining, err := p.Leave(ctx, "doc-a", "c1")
	if err != nil || !slices.Equal(remaining, []string{"c2", "c3"}) {
		t.Fatalf("leave = %v, %v; want [c2 c3]", remaining, err)
	}
	if _, err := p.Join(ctx, "doc-b", "z1"); err != nil {
		t.Fatalf("doc-b join: %v", err)
	}
	if got, err := p.List(ctx, "doc-a"); err != nil || slices.Contains(got, "z1") {
		t.Fatalf("doc-b leaked into doc-a: %v, %v", got, err)
	}
}

// TestDocPresenceCrashIsIsolated pins let-it-crash: a crashed doc restarts
// with a new generation and an empty soft roster; its neighbour is untouched.
func TestDocPresenceCrashIsIsolated(t *testing.T) {
	ctx, host := testHost(t)
	p := NewDocPresence(host)
	if _, err := p.Join(ctx, "doc-a", "c1"); err != nil {
		t.Fatalf("doc-a join: %v", err)
	}
	if _, err := p.Join(ctx, "doc-b", "z1"); err != nil {
		t.Fatalf("doc-b join: %v", err)
	}
	before, err := p.Snapshot(ctx, "doc-a")
	if err != nil {
		t.Fatalf("snapshot before: %v", err)
	}
	beforeB, err := p.Snapshot(ctx, "doc-b")
	if err != nil {
		t.Fatalf("doc-b snapshot before: %v", err)
	}
	if cerr := p.Crash(ctx, "doc-a"); cerr != nil {
		t.Fatalf("crash: %v", cerr)
	}
	deadline := time.Now().Add(5 * time.Second)
	for {
		after, lerr := p.Snapshot(ctx, "doc-a")
		if lerr == nil && after.Generation > before.Generation {
			break
		}
		if time.Now().After(deadline) {
			t.Fatalf("doc-a did not restart: %+v, %v", after, lerr)
		}
		time.Sleep(20 * time.Millisecond)
	}
	afterB, err := p.Snapshot(ctx, "doc-b")
	if err != nil {
		t.Fatalf("doc-b snapshot after: %v", err)
	}
	if !slices.Equal(beforeB.Peers, afterB.Peers) || beforeB.Generation != afterB.Generation {
		t.Fatalf("doc-b changed by doc-a crash: before %+v after %+v", beforeB, afterB)
	}
	if _, err := p.Join(ctx, "doc-a", "c1"); err != nil {
		t.Fatalf("rejoin after crash: %v", err)
	}
	if got, err := p.List(ctx, "doc-a"); err != nil || !slices.Equal(got, []string{"c1"}) {
		t.Fatalf("doc-a after rejoin = %v, %v", got, err)
	}
}
