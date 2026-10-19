// SCOPE:plugin — Turso snapshot persister (production flat-stack store)
package collab

import (
	"context"
	"errors"
	"fmt"

	dbgen "github.com/calionauta/gogogo/internal/db/sqlcgen"
	"github.com/calionauta/gogogo/internal/dbschema"
)

// TursoPersister implements Persister on the doc_snapshots table: one row
// per doc, upserted on every save. Snapshots are opaque bytes to this
// layer (Loro-encoded); the table never interprets them.
type TursoPersister struct {
	q *dbgen.Queries
}

// NewTursoPersister builds the production persister on migrated queries.
func NewTursoPersister(q *dbgen.Queries) (*TursoPersister, error) {
	if q == nil {
		return nil, errors.New("collab: nil queries")
	}
	return &TursoPersister{q: q}, nil
}

// SaveSnapshot upserts the resolved snapshot for docID.
func (p *TursoPersister) SaveSnapshot(docID string, snapshot []byte) error {
	if err := p.q.UpsertSnapshot(context.Background(), dbgen.UpsertSnapshotParams{
		DocID: docID, Snapshot: snapshot, Updated: dbschema.Now(),
	}); err != nil {
		return fmt.Errorf("collab: save snapshot %s: %w", docID, err)
	}
	return nil
}

// LoadSnapshot returns the persisted snapshot for docID, or (nil, false)
// when the doc has never been saved.
func (p *TursoPersister) LoadSnapshot(docID string) ([]byte, bool) {
	row, err := p.q.GetSnapshot(context.Background(), docID)
	if err != nil {
		return nil, false
	}
	if len(row.Snapshot) == 0 {
		return nil, false
	}
	return row.Snapshot, true
}

// DeleteSnapshot drops the persisted snapshot for a removed doc. The
// snapshot table has no foreign key to the metadata rows (it is keyed by
// the opaque doc id only), so the caller deletes both halves; a doc that
// was created and never written to has no row here and deleting it is a
// no-op, not an error.
func (p *TursoPersister) DeleteSnapshot(docID string) error {
	if err := p.q.DeleteSnapshot(context.Background(), docID); err != nil {
		return fmt.Errorf("collab: delete snapshot %s: %w", docID, err)
	}
	return nil
}

// Compile-time guard: TursoPersister satisfies Persister wherever the
// in-memory fake or the previous-stack persister was accepted.
var _ Persister = (*TursoPersister)(nil)
