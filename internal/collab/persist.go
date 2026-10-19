// SCOPE:plugin — Snapshot persisters (memory + Turso)
package collab

import (
	"sync"
)

// Persister stores the resolved snapshot of a collaborative doc, keyed
// by doc id. Production uses the Turso persister below; tests use the
// in-memory fake.
//
// DeleteSnapshot exists because a doc can be removed from the index: the
// metadata row and its snapshot are two stores, and deleting only the row
// leaves content nobody can list but the server would still rehydrate for
// anyone holding the URL. Deleting a doc with no snapshot is not an error
// (a board created and never drawn on never persisted one).
type Persister interface {
	SaveSnapshot(docID string, snapshot []byte) error
	LoadSnapshot(docID string) ([]byte, bool)
	DeleteSnapshot(docID string) error
}

// MemoryPersister is an in-memory Persister for tests.
type MemoryPersister struct {
	mu    sync.Mutex
	store map[string][]byte
}

// NewMemoryPersister returns an empty in-memory persister.
func NewMemoryPersister() *MemoryPersister {
	return &MemoryPersister{store: make(map[string][]byte)}
}

// SaveSnapshot stores the latest snapshot for the doc.
func (m *MemoryPersister) SaveSnapshot(docID string, snapshot []byte) error {
	m.mu.Lock()
	defer m.mu.Unlock()
	cp := make([]byte, len(snapshot))
	copy(cp, snapshot)
	m.store[docID] = cp
	return nil
}

// LoadSnapshot returns the stored snapshot (nil if none) and whether one
// was present — used to rehydrate a doc on open.
func (m *MemoryPersister) LoadSnapshot(docID string) ([]byte, bool) {
	m.mu.Lock()
	defer m.mu.Unlock()
	s, ok := m.store[docID]
	return s, ok
}

// DeleteSnapshot drops the stored snapshot for a removed doc. Deleting a
// doc that never persisted a snapshot is a no-op, not an error.
func (m *MemoryPersister) DeleteSnapshot(docID string) error {
	m.mu.Lock()
	defer m.mu.Unlock()
	delete(m.store, docID)
	return nil
}
