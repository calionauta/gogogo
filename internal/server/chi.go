// SCOPE:core — Flat-stack chi server (staged-migration seam)
//
// StartChi serves the flat-stack chi router (owned auth + sqlc todos)
// when CHI_ADDR is set, alongside the default boot. Unset CHI_ADDR is a
// no-op returning nil, so a stock env never changes behaviour.
package server

import (
	"context"
	"database/sql"
	"fmt"
	"log/slog"
	"net"
	"net/http"
	"time"

	"github.com/calionauta/gogogo/config"
	configchi "github.com/calionauta/gogogo/features/config/chi"
	landingchi "github.com/calionauta/gogogo/features/landing/chi"
	notesachi "github.com/calionauta/gogogo/features/notes/chi"
	"github.com/calionauta/gogogo/features/todo/chi"
	wbchi "github.com/calionauta/gogogo/features/whiteboard/chi"
	"github.com/calionauta/gogogo/internal/auth"
	"github.com/calionauta/gogogo/internal/collab"
	"github.com/calionauta/gogogo/internal/db"
	dbgen "github.com/calionauta/gogogo/internal/db/sqlcgen"
	"github.com/calionauta/gogogo/internal/nats"
	"github.com/calionauta/gogogo/internal/natswire"
	"github.com/calionauta/gogogo/internal/queue"
)

// shutdownTimeout bounds chi-stack shutdown: in-flight SSE streams get a
// few seconds to finish before the process exits.
const shutdownTimeout = 5 * time.Second

// StartChi boots the flat stack and serves it in the background. The
// returned func stops the server; callers defer it. A nil return with a
// nil error means "seam disabled" (no CHI_ADDR) — so a stock env never
// changes behaviour.
//
// js carries the JetStream context for cross-instance convergence (nil =
// Hub-only, same as NATS_ENABLED=false). The consumer runs until stop.
func StartChi(cfg *config.Config, js nats.JetStreamLike) (stop func(), err error) {
	if cfg.ChiAddr == "" {
		return nil, nil
	}

	bundle, err := bootChi(cfg, js)
	if err != nil {
		return nil, err
	}

	ln, err := (&net.ListenConfig{}).Listen(context.Background(), "tcp", cfg.ChiAddr)
	if err != nil {
		bundle.cancelConsumer()
		_ = bundle.sqlDB.Close()
		return nil, fmt.Errorf("server: chi listen %s: %w", cfg.ChiAddr, err)
	}
	return serveChiOnListener(ln, srvHandler(bundle.mux), bundle.cancelConsumer, bundle.sqlDB), nil
}

// chiBundle is everything StartChi builds before listening. Tests boot it
// and serve their own :0 listener, driving the production composition over
// real HTTP — the automated preflight.
type chiBundle struct {
	sqlDB          *sql.DB
	mux            http.Handler
	cancelConsumer context.CancelFunc
}

func bootChi(cfg *config.Config, js nats.JetStreamLike) (*chiBundle, error) {
	sqlDB, err := db.Open(cfg)
	if err != nil {
		return nil, fmt.Errorf("server: chi db open: %w", err)
	}
	if err := db.Migrate(sqlDB); err != nil {
		_ = sqlDB.Close()
		return nil, fmt.Errorf("server: chi db migrate: %w", err)
	}

	a, aerr := auth.New(dbgen.New(sqlDB), !cfg.Dev)
	if aerr != nil {
		_ = sqlDB.Close()
		return nil, fmt.Errorf("server: chi auth: %w", aerr)
	}

	hub := queue.NewSSEHub()
	queries := dbgen.New(sqlDB)
	todoH := chi.NewHandler(queries, a, hub, cfg.Skin)
	todoH.SetPublisher(natswire.NewCrudPublisher(js))
	todoH.BuildLabel = cfg.BuildLabel
	todoH.BuildCommit = cfg.BuildCommit
	todoH.OfflineSyncEnabled = cfg.OfflineSync.Enabled
	todoH.NATSEnabled = cfg.NATS.Enabled

	bootCtx, cancelConsumer := context.WithCancel(context.Background())
	go func() {
		if cerr := chi.RunCrudConsumer(bootCtx, js, cfg.AppName, queries, hub); cerr != nil {
			slog.Error("server: chi crud consumer stopped", "error", cerr)
		}
	}()

	persister, perr := collab.NewTursoPersister(queries)
	if perr != nil {
		cancelConsumer()
		_ = sqlDB.Close()
		return nil, fmt.Errorf("server: chi snapshots: %w", perr)
	}
	docs := collab.NewDocStore()
	docs.SetPersister(persister)
	return &chiBundle{sqlDB: sqlDB, mux: buildChiMux(a, todoH,
		landingchi.NewHandler(cfg, a), configchi.NewHandler(cfg, a),
		wbchi.NewHandler(queries, a, hub, persister, docs, cfg),
		notesachi.NewHandler(queries, a, hub, persister, docs, cfg),
	), cancelConsumer: cancelConsumer}, nil
}

// srvHandler wraps the mux for serveChiOnListener (kept separate so the
// listener stays the caller's: tests bind :0 and read the port back).
func srvHandler(mux http.Handler) *http.Server {
	return &http.Server{
		Handler:           mux,
		ReadHeaderTimeout: 10 * time.Second,
	}
}

// serveChiOnListener serves an already-bound listener until stop. Tests
// bind 127.0.0.1:0 and drive real HTTP through ln.Addr — the automated
// preflight for the serve path manual runs used to cover.
func serveChiOnListener(ln net.Listener, srv *http.Server, cancelConsumer context.CancelFunc, sqlDB *sql.DB) func() {
	go func() {
		if serr := srv.Serve(ln); serr != nil && serr != http.ErrServerClosed {
			slog.Error("server: chi stack stopped", "error", serr)
		}
	}()
	slog.Info("server: chi flat stack listening", "addr", ln.Addr())
	return func() {
		cancelConsumer()
		ctx, cancel := context.WithTimeout(context.Background(), shutdownTimeout)
		defer cancel()
		_ = srv.Shutdown(ctx)
		_ = sqlDB.Close()
	}
}

// buildChiMux composes the flat-stack routes. Pure constructor (no ports,
// no goroutines) so contract tests cover the exact composition production
// serves.
func buildChiMux(
	a *auth.Auth, todoH *chi.Handler,
	landingH *landingchi.Handler, configH *configchi.Handler,
	wbH *wbchi.Handler, notesH *notesachi.Handler,
) http.Handler {
	// Every sub-router serves absolute paths (the auth handler carries
	// its /api/auth base path; chi routers match full paths too), and
	// the std mux never strips prefixes — so each subtree pattern routes
	// to the handler owning those absolute routes. Longest match wins;
	// the landing root is the catch-all.
	mux := http.NewServeMux()
	mux.Handle("/api/auth/", a.Handler())
	mux.Handle("/api/todos", todoH.Routes())
	mux.Handle("/api/todos/", todoH.Routes())
	mux.Handle("/todo", todoH.Routes())
	mux.Handle("/todo/", todoH.Routes())
	mux.Handle("/login", todoH.Routes())
	mux.Handle("/logout", todoH.Routes())
	mux.Handle("/config", configH.Routes())
	mux.Handle("/config/", configH.Routes())
	mux.Handle("/whiteboard", wbH.Routes())
	mux.Handle("/whiteboard/", wbH.Routes())
	mux.Handle("/api/whiteboard", wbH.Routes())
	mux.Handle("/api/whiteboard/", wbH.Routes())
	mux.Handle("/api/whiteboards/", wbH.Routes())
	mux.Handle("/notes", notesH.Routes())
	mux.Handle("/notes/", notesH.Routes())
	mux.Handle("/api/notes", notesH.Routes())
	mux.Handle("/api/notes/", notesH.Routes())
	mux.Handle("/", landingH.Routes())
	return mux
}
