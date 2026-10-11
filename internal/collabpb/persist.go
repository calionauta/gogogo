// SCOPE:plugin — PocketBase snapshot persister (dies with the framework)
//
// Transitional: the flat stack persists snapshots to Turso
// (collab.TursoPersister). This file serves the previous stack only and
// goes away with it.
package collabpb

import (
	"fmt"

	"github.com/pocketbase/pocketbase/core"
)

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
