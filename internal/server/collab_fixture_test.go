package server

import (
	"database/sql"
	"testing"

	"github.com/calionauta/gogogo/internal/collab"
	dbgen "github.com/calionauta/gogogo/internal/db/sqlcgen"
)

// collabTransportFixture builds the CRDT transport for tests that need the full
// Deps value, or returns nil in a tree trimmed without collab.
//
// It lives in its own file on purpose: the tests that use it assert unrelated
// things (a route answers 404, a session gate rejects), and building the
// persister inline made each of them the last importer of a package a trim
// deletes. A separate file is one declaration to remove with the transport, so
// the trim keeps those tests and this file leaves with collab.
func collabTransportFixture(t *testing.T, sqlDB *sql.DB) any {
	t.Helper()
	persister, err := collab.NewTursoPersister(dbgen.New(sqlDB))
	if err != nil {
		t.Fatalf("persister: %v", err)
	}
	docs := collab.NewDocStore()
	docs.SetPersister(persister)
	return collab.Handoff{Persister: persister, Docs: docs}
}
