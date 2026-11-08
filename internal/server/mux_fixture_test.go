// The shared composed-tree fixture for server tests: an opened auth
// stack over a migrated file DB, the todo handler, and buildChiMux's
// day-one wiring on the production call order.
//
// One builder because three tests verify the same composition through
// it; a copy per test would drift the way the anchor table did.
package server

import (
	"database/sql"
	"testing"

	"github.com/calionauta/gogogo/config"
	chathandlers "github.com/calionauta/gogogo/features/chat/handlers"
	chatstore "github.com/calionauta/gogogo/features/chat/store"
	creditshandlers "github.com/calionauta/gogogo/features/credits/handlers"
	noteshandlers "github.com/calionauta/gogogo/features/notes/handlers"
	roomhandlers "github.com/calionauta/gogogo/features/room/handlers"
	todohandlers "github.com/calionauta/gogogo/features/todo/handlers"
	wbhandlers "github.com/calionauta/gogogo/features/whiteboard/handlers"
	"github.com/calionauta/gogogo/internal/auth"
	"github.com/calionauta/gogogo/internal/collab"
	"github.com/calionauta/gogogo/internal/db"
	dbgen "github.com/calionauta/gogogo/internal/db/sqlcgen"
	"github.com/calionauta/gogogo/internal/dbschema"
	"github.com/calionauta/gogogo/internal/queue"
	"github.com/calionauta/gogogo/internal/recipe/register"
)

type muxFixture struct {
	mux   *Router
	auth  *auth.Auth
	sqlDB *sql.DB
	cfg   *config.Config
}

func buildMuxFixture(t *testing.T) *muxFixture {
	t.Helper()
	sqlDB, err := db.OpenAt(t.TempDir() + "/mux.db")
	if err != nil {
		t.Fatalf("db open: %v", err)
	}
	t.Cleanup(func() { sqlDB.Close() })
	if err := dbschema.Migrate(sqlDB); err != nil {
		t.Fatalf("db migrate: %v", err)
	}
	a, aerr := auth.New(dbgen.New(sqlDB), false)
	if aerr != nil {
		t.Fatalf("auth new: %v", aerr)
	}
	todoH := todohandlers.NewHandler(dbgen.New(sqlDB), a, queue.NewSSEHub())
	cfg := config.Load()
	// Retained for combined trims (same rule as the bootCtx retain in chi.go).
	_ = cfg
	persister, perr := collab.NewTursoPersister(dbgen.New(sqlDB))
	if perr != nil {
		t.Fatalf("persister: %v", perr)
	}
	docs := collab.NewDocStore()
	reviewH := newReviewHandlerForTest(cfg, a)
	// SQLDB is what the recipe step migrates: without it a recipe that ships
	// DDL cannot apply its schema, and the composition below would be built
	// with the recipe step failed and every seam-mounted route (todo's
	// /login among them) silently missing.
	mux := buildChiMux(a, register.Deps{
		Cfg: cfg, Auth: a, Queries: dbgen.New(sqlDB),
		Hub: queue.NewSSEHub(), SQLDB: sqlDB,
	}, todoH,
		wbhandlers.NewHandler(dbgen.New(sqlDB), a, queue.NewSSEHub(), persister, docs, cfg),
		noteshandlers.NewHandler(dbgen.New(sqlDB), a, queue.NewSSEHub(), persister, docs, cfg),
		roomhandlers.NewHandler(cfg, a, fakeRooms{}, queue.NewSSEHub()),
		chathandlers.NewHandler(cfg, a, chiQForTest(t), queue.NewSSEHub(), chatstore.NewMemStore(),
			stubResponder{raw: "{}"}),
		reviewH,
		creditshandlers.NewHandler(a, nil))
	if mux.recipeErr != nil {
		t.Fatalf("recipe boot: %v", mux.recipeErr)
	}
	return &muxFixture{mux: mux, auth: a, sqlDB: sqlDB, cfg: cfg}
}
