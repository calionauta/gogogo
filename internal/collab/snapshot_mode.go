// SCOPE:plugin — Loro CRDT + DocStore + sync workers + presence
package collab

import (
	"fmt"
	"strings"

	dbgen "github.com/calionauta/gogogo/internal/db/sqlcgen"
)

// SnapshotStore selects the CRDT snapshot backend from the COLLAB_SNAPSHOTS
// value ("turso" keeps snapshots in doc_snapshots; "memory" keeps them
// process-local — draw without persistence). Empty spells the default
// (turso). Unknown values fail closed: a typo must not silently downgrade
// durability. Lives here (not in internal/server) so trimming the
// whiteboard bundle deletes it with the package — the composition root
// keeps only line-drops, never a block strip reaching outside the bundle.
func SnapshotStore(mode string, queries *dbgen.Queries) (Persister, error) {
	switch strings.ToLower(strings.TrimSpace(mode)) {
	case "", "turso":
		p, err := NewTursoPersister(queries)
		if err != nil {
			return nil, fmt.Errorf("server: chi snapshots: %w", err)
		}
		return p, nil
	case "memory":
		return NewMemoryPersister(), nil
	default:
		return nil, fmt.Errorf("server: unknown COLLAB_SNAPSHOTS %q (want turso|memory)", mode)
	}
}
