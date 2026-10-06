// SCOPE:layer=infra,removal=plugin — Loro CRDT + DocStore + sync workers + presence
package collab

import (
	"log/slog"
	"sync"
	"time"
)

// Default bounds for the doc cache. These exist because the store previously
// grew without limit: one LoroDoc per docID (whiteboard) or per owner
// (crdtstore), retained for the lifetime of the process. A LoroDoc holds the
// full op log, so on a long-lived server that is an unbounded memory leak
// proportional to how many docs have ever been touched.
const (
	// DefaultMaxDocs bounds how many docs stay resident.
	DefaultMaxDocs = 256

	// DefaultIdleTTL is the minimum idle time before a doc is evictable.
	//
	// This is what makes eviction safe without reference counting: a single
	// request mutates a doc for microseconds, so a doc idle for minutes cannot
	// be mid-mutation. Without a floor, an LRU could evict a doc between the
	// "get" and the "apply" of the same request, and two concurrent ops on the
	// same doc would then land on two different Doc instances — silently losing
	// one. The TTL makes that window practically unreachable while keeping the
	// cache bounded.
	DefaultIdleTTL = 5 * time.Minute
)

// DocStore is a shared, thread-safe, BOUNDED repository of collaborative CRDT
// docs. Both WebSyncWorker (SSE Hub transport + NATS publishing) and
// SyncWorker (NATS subscriber) reference the same DocStore, so applying a shape
// op from one browser instance converges the same Doc that a NATS-delivered
// update from another process mutates — no split-brain.
//
// On a cache miss the doc is REHYDRATED from the Persister before it is
// returned. That is not an optimisation, it is a correctness requirement: a
// request that arrives for a doc this process has never loaded must not start
// from an empty doc, or the op is applied to nothing and the resolved snapshot
// is then persisted over the good one. That path is reachable in production —
// the service worker serves the board page from cache after a server restart,
// the SSE stream connects (loading nothing), and the client's IndexedDB outbox
// then replays an op into an empty doc, discarding every shape drawn before.
type DocStore struct {
	mu   sync.Mutex
	docs map[string]*Doc

	// lastUsed drives LRU eviction. time.Time rather than a counter because
	// the eviction rule is "idle for longer than IdleTTL", not "oldest of N".
	lastUsed map[string]time.Time

	// persister rehydrates a doc on a cache miss. Optional: a nil persister
	// means an in-memory-only store (tests, SSE-only demos) and a miss starts
	// empty, which is the old behaviour.
	persister Persister

	maxDocs int
	idleTTL time.Duration

	// overCapacityWarned suppresses repeat logging of the over-capacity state.
	// The condition persists for as long as the workload does (every new doc
	// re-triggers it), so logging per call would flood the log with the same
	// line. Warn once on entry and reset when the cache is back within bounds,
	// so a genuinely sustained problem is visible without being spam.
	overCapacityWarned bool
}

// NewDocStore creates an empty store with the default bounds and no persister.
// Prefer NewDocStoreWithPersister in production so cache misses rehydrate.
func NewDocStore() *DocStore {
	return &DocStore{
		docs:     make(map[string]*Doc),
		lastUsed: make(map[string]time.Time),
		maxDocs:  DefaultMaxDocs,
		idleTTL:  DefaultIdleTTL,
	}
}

// NewDocStoreWithPersister creates a store that rehydrates docs from p on a
// cache miss. maxDocs <= 0 uses DefaultMaxDocs; idleTTL <= 0 uses DefaultIdleTTL.
func NewDocStoreWithPersister(p Persister, maxDocs int, idleTTL time.Duration) *DocStore {
	s := NewDocStore()
	s.persister = p
	if maxDocs > 0 {
		s.maxDocs = maxDocs
	}
	if idleTTL > 0 {
		s.idleTTL = idleTTL
	}
	return s
}

// SetPersister wires the rehydration source after construction. Used by the
// sync workers, which receive the store and the persister as separate
// arguments, so the store learns where to reload from.
func (s *DocStore) SetPersister(p Persister) {
	s.mu.Lock()
	defer s.mu.Unlock()
	s.persister = p
}

// GetOrCreate returns the Doc for id, rehydrating it from the Persister on a
// cache miss, and records the access for LRU eviction.
//
// Returns the same *Doc for concurrent callers, so a doc is never duplicated
// while resident.
func (s *DocStore) GetOrCreate(id string) *Doc {
	s.mu.Lock()
	defer s.mu.Unlock()

	if d, ok := s.docs[id]; ok {
		s.lastUsed[id] = time.Now()
		return d
	}

	d := NewDoc(id)
	// Rehydrate BEFORE publishing the doc into the map, so no concurrent
	// caller can observe (and mutate) a half-loaded doc.
	if s.persister != nil {
		if snap, ok := s.persister.LoadSnapshot(id); ok && len(snap) > 0 {
			if err := d.ApplyUpdate(snap); err != nil {
				// A corrupt snapshot must not take the doc out of service:
				// log and start empty, which is the previous behaviour.
				slog.Warn("collab: rehydrate failed, starting empty",
					"doc", id, "error", err)
			}
		}
	}

	s.docs[id] = d
	s.lastUsed[id] = time.Now()
	s.evictLocked()
	return d
}

// Len reports how many docs are resident. Exposed for tests and metrics.
func (s *DocStore) Len() int {
	s.mu.Lock()
	defer s.mu.Unlock()
	return len(s.docs)
}

// Evict drops a doc from the cache. The persisted snapshot is untouched, so a
// later GetOrCreate rehydrates it. Safe to call for a doc that is not resident.
func (s *DocStore) Evict(id string) {
	s.mu.Lock()
	defer s.mu.Unlock()
	delete(s.docs, id)
	delete(s.lastUsed, id)
}

// evictLocked drops idle docs until the cache is within maxDocs. Callers must
// hold s.mu.
//
// A doc is only evictable once it has been idle for idleTTL, so a doc that is
// being used right now is never dropped even if that means temporarily
// exceeding the cap. Exceeding the cap is the correct trade: the alternative is
// evicting a doc mid-request and losing an op.
func (s *DocStore) evictLocked() {
	if len(s.docs) <= s.maxDocs {
		return
	}
	cutoff := time.Now().Add(-s.idleTTL)

	// Collect evictable docs (oldest first). The set is small (bounded by
	// maxDocs plus whatever is active), so a sort is cheaper than maintaining
	// a second index for every access.
	type cand struct {
		id string
		at time.Time
	}
	var cands []cand
	for id, at := range s.lastUsed {
		if at.Before(cutoff) {
			cands = append(cands, cand{id, at})
		}
	}
	if len(cands) == 0 {
		// Everything is active. Do not evict; report it ONCE so an operator can
		// see the cache genuinely needs a larger bound rather than silently
		// growing. Repeated per-call logging would flood the log, since every
		// new doc re-triggers this while the workload continues.
		if !s.overCapacityWarned {
			slog.Warn("collab: doc cache over capacity with no idle docs to evict",
				"resident", len(s.docs), "max", s.maxDocs, "idle_ttl", s.idleTTL)
			s.overCapacityWarned = true
		}
		return
	}
	s.overCapacityWarned = false
	// Insertion sort: the slice is small and this avoids pulling in sort for
	// one call site.
	for i := 1; i < len(cands); i++ {
		for j := i; j > 0 && cands[j].at.Before(cands[j-1].at); j-- {
			cands[j], cands[j-1] = cands[j-1], cands[j]
		}
	}
	for _, c := range cands {
		if len(s.docs) <= s.maxDocs {
			break
		}
		delete(s.docs, c.id)
		delete(s.lastUsed, c.id)
	}
}
