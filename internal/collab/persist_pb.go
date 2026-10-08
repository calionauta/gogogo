// SCOPE:layer=infra,removal=plugin — Loro CRDT + DocStore + sync workers + presence
// Package collab wraps the Loro CRDT (github.com/aholstenson/loro-go) for
// the template's collaborative features (whiteboard, shared docs).
//
// This file is build-tag-free so the PocketBase-backed persister is
// available to the web-only transport (sync_web.go) as well as the
// jetstream-tagged SyncWorker (sync.go). Each feature converges onto its
// own collection ("whiteboards", "notes", ...) as the source of truth.
package collab

import (
	"fmt"
	"sync"

	"github.com/pocketbase/pocketbase/core"
)

// Persister stores the resolved snapshot of a collaborative doc. The
// server-side implementation writes to one PocketBase collection per
// feature ("whiteboards", "notes"); tests use an in-memory fake.
type Persister interface {
	SaveSnapshot(docID string, snapshot []byte) error
	LoadSnapshot(docID string) ([]byte, bool)
}

// MemoryPersister is an in-memory Persister for tests and local demos
// that don't need PocketBase durability.
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

// PocketBasePersister implements Persister against one collection.
// Upsert by doc_id; stores the Loro snapshot as base64 in the
// "snapshot" field and a monotonic "version" for idempotent writes.
type PocketBasePersister struct {
	app        core.App
	collection string
}

// NewPocketBasePersister builds a persister for the given PocketBase app,
// targeting the "whiteboards" collection.
func NewPocketBasePersister(app core.App) *PocketBasePersister {
	return NewPocketBasePersisterForCollection(app, "whiteboards")
}

// NewPocketBasePersisterForCollection builds a persister targeting the
// named collection (same schema: doc_id, snapshot, version). The notes
// feature uses it with the "notes" collection.
func NewPocketBasePersisterForCollection(app core.App, collection string) *PocketBasePersister {
	return &PocketBasePersister{app: app, collection: collection}
}

// SaveSnapshot upserts the record keyed by docID.
func (p *PocketBasePersister) SaveSnapshot(docID string, snapshot []byte) error {
	// See db/seed.go ensureWhiteboardsCollection / ensureNotesCollection
	// for the schema:
	//   doc_id (text, unique), snapshot (text/base64), version (int).
	col, err := p.app.FindCollectionByNameOrId(p.collection)
	if err != nil {
		return fmt.Errorf("%s collection: %w", p.collection, err)
	}
	rec, err := p.app.FindFirstRecordByFilter(p.collection, "doc_id = {:doc}", map[string]any{"doc": docID})
	if err != nil || rec == nil {
		rec = core.NewRecord(col)
		rec.Set("doc_id", docID)
		rec.Set("version", 1)
	} else {
		rec.Set("version", rec.GetInt("version")+1)
	}
	rec.Set("snapshot", string(snapshot))
	if err := p.app.Save(rec); err != nil {
		return fmt.Errorf("save %s %s: %w", p.collection, docID, err)
	}
	return nil
}

// LoadSnapshot returns the persisted Loro snapshot for docID, or
// (nil, false) when the doc has never been saved.
func (p *PocketBasePersister) LoadSnapshot(docID string) ([]byte, bool) {
	rec, err := p.app.FindFirstRecordByFilter(p.collection, "doc_id = {:doc}", map[string]any{"doc": docID})
	if err != nil || rec == nil {
		return nil, false
	}
	snap := rec.GetString("snapshot")
	if snap == "" {
		return nil, false
	}
	return []byte(snap), true
}
