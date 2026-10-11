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
	"os"
	"path/filepath"
	"time"

	"github.com/calionauta/gogogo/config"
	chatchi "github.com/calionauta/gogogo/features/chat/chi"
	chatstore "github.com/calionauta/gogogo/features/chat/store"
	configchi "github.com/calionauta/gogogo/features/config/chi"
	"github.com/calionauta/gogogo/features/credits"
	creditschi "github.com/calionauta/gogogo/features/credits/chi"
	genuichi "github.com/calionauta/gogogo/features/genui/chi"
	comp "github.com/calionauta/gogogo/features/genui/components"
	landingchi "github.com/calionauta/gogogo/features/landing/chi"
	notesachi "github.com/calionauta/gogogo/features/notes/chi"
	roomchi "github.com/calionauta/gogogo/features/room/chi"
	"github.com/calionauta/gogogo/features/todo/chi"
	wbchi "github.com/calionauta/gogogo/features/whiteboard/chi"
	"github.com/calionauta/gogogo/internal/auth"
	"github.com/calionauta/gogogo/internal/collab"
	"github.com/calionauta/gogogo/internal/db"
	dbgen "github.com/calionauta/gogogo/internal/db/sqlcgen"
	appgoakt "github.com/calionauta/gogogo/internal/goakt"
	"github.com/calionauta/gogogo/internal/llm"
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
	return serveChiOnListener(ln, srvHandler(bundle.mux), bundle), nil
}

// bootCredits opens the Turso ledger, builds the engine (nil when the
// feature is disabled — routes answer 503), starts its workers and
// registers its shutdown. Split from bootChi to keep both under the
// function-length gate.
func bootCredits(
	ctx context.Context, cfg *config.Config, shutdown []func(),
	sqlDB *sql.DB, cancelConsumer context.CancelFunc, a *auth.Auth,
) (*creditschi.Handler, []func(), error) {
	ledgerDB, lerr := creditschi.OpenTursoLedger(cfg.DataDir)
	if lerr != nil {
		cancelConsumer()
		_ = sqlDB.Close()
		return nil, shutdown, fmt.Errorf("server: chi ledger: %w", lerr)
	}
	shutdown = append(shutdown, func() { _ = ledgerDB.Close() })
	//nolint:contextcheck // engine constructor takes no context (same as the
	// previous stack's wiring); cancellation starts at Service.Start.
	creditsSvc, cerr := credits.NewOnDB(ledgerDB, cfg)
	if cerr != nil {
		cancelConsumer()
		_ = sqlDB.Close()
		return nil, shutdown, fmt.Errorf("server: chi credits: %w", cerr)
	}
	if creditsSvc == nil {
		return creditschi.NewHandler(a, nil), shutdown, nil
	}
	creditsSvc.Start(ctx)
	return creditschi.NewHandler(a, creditsSvc), shutdown, nil
}

// chiBundle is everything StartChi builds before listening. Tests boot it
// and serve their own :0 listener, driving the production composition over
// real HTTP — the automated preflight.
type chiBundle struct {
	sqlDB          *sql.DB
	mux            http.Handler
	cancelConsumer context.CancelFunc
	// shutdown runs on stop, after the HTTP server drains: queue close,
	// actor halt. Tests get the same cleanup through serveChiOnListener.
	shutdown []func()
}

func bootChi(cfg *config.Config, js nats.JetStreamLike) (*chiBundle, error) {
	sqlDB, err := db.Open(cfg)
	if err != nil {
		return nil, fmt.Errorf("server: chi db open: %w", err)
	}
	if merr := db.Migrate(sqlDB); merr != nil {
		_ = sqlDB.Close()
		return nil, fmt.Errorf("server: chi db migrate: %w", merr)
	}

	a, aerr := auth.New(dbgen.New(sqlDB), !cfg.Dev)
	if aerr != nil {
		_ = sqlDB.Close()
		return nil, fmt.Errorf("server: chi auth: %w", aerr)
	}

	bootCtx, cancelConsumer := context.WithCancel(context.Background())
	var shutdown []func()

	hub := queue.NewSSEHub()
	queries := dbgen.New(sqlDB)
	todoH := chi.NewHandler(queries, a, hub, cfg.Skin)
	todoH.SetPublisher(natswire.NewCrudPublisher(js))
	todoH.BuildLabel = cfg.BuildLabel
	todoH.BuildCommit = cfg.BuildCommit
	todoH.OfflineSyncEnabled = cfg.OfflineSync.Enabled
	todoH.NATSEnabled = cfg.NATS.Enabled

	// GoAkt room grains. A failed start degrades to 502s (same as the
	// previous stack with its engine down), never to a failed boot.
	var roomH *roomchi.Handler
	if cfg.GoAkt.Enabled {
		host := appgoakt.New()
		if herr := host.Start(bootCtx); herr != nil {
			slog.Warn("server: goakt failed to start, room demo disabled", "error", herr)
		} else {
			appgoakt.SetDefault(host)
			roomH = roomchi.NewHandler(cfg, a, host)
			shutdown = append(shutdown, func() {
				ctx, cancel := context.WithTimeout(context.Background(), shutdownTimeout)
				defer cancel()
				_ = host.Stop(ctx)
			})
		}
	}

	// Chi-owned queue: chat/genui workers run here so they never compete
	// with the other stack's workers for the same jobs.
	chiQ, qerr := queue.NewAt(filepath.Join(cfg.DataDir, "chi"))
	if qerr != nil {
		cancelConsumer()
		_ = sqlDB.Close()
		return nil, fmt.Errorf("server: chi queue: %w", qerr)
	}
	shutdown = append(shutdown, chiQ.Close)

	realLLM := llm.New(cfg.GoAI.APIKey)
	var simulatedLLM *llm.Client
	if v := os.Getenv("SIMULATE_LLM"); v != "false" {
		simulatedLLM = llm.NewSimulated()
	}
	chatH := chatchi.NewHandler(cfg, a, chiQ, queue.NewSSEHub(),
		chatstore.NewMemStore(), chiChatResponder(realLLM, simulatedLLM))
	chatH.RegisterHandlers(chiQ.Registry())
	genuiH := genuichi.NewHandler(cfg, a, chiQ, queue.NewSSEHub(),
		comp.NewResponder(realLLM, simulatedLLM))
	genuiH.RegisterHandlers(chiQ.Registry())
	todoH.SetQueue(chiQ)
	todoH.SetLLMClient(realLLM)
	if simulatedLLM != nil {
		todoH.SetSimulatedLLMClient(simulatedLLM)
	}
	todoH.RegisterSuggestHandlers(chiQ.Registry())
	todoH.RegisterRetryHandlers(chiQ.Registry())
	chiQ.StartWorkers()

	creditsH, shutdown, err := bootCredits(bootCtx, cfg, shutdown, sqlDB, cancelConsumer, a)
	if err != nil {
		return nil, err
	}

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
		roomH, chatH, genuiH, creditsH,
	), cancelConsumer: cancelConsumer, shutdown: shutdown}, nil
}

// chiChatResponder bridges the genui model leg to the chat Responder:
// identical method shape, distinct named types so neither feature imports
// the other's handler.
func chiChatResponder(real, simulated *llm.Client) chatstore.Responder {
	return chiResponderAdapter{inner: comp.NewResponder(real, simulated)}
}

// chiResponderAdapter adapts comp.Responder to the chat store seam.
type chiResponderAdapter struct {
	inner comp.Responder
}

func (ad chiResponderAdapter) Respond(ctx context.Context, prompt string) (string, error) {
	return ad.inner.Respond(ctx, prompt)
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
func serveChiOnListener(ln net.Listener, srv *http.Server, bundle *chiBundle) func() {
	go func() {
		if serr := srv.Serve(ln); serr != nil && serr != http.ErrServerClosed {
			slog.Error("server: chi stack stopped", "error", serr)
		}
	}()
	slog.Info("server: chi flat stack listening", "addr", ln.Addr())
	return func() {
		bundle.cancelConsumer()
		ctx, cancel := context.WithTimeout(context.Background(), shutdownTimeout)
		defer cancel()
		_ = srv.Shutdown(ctx)
		for _, stop := range bundle.shutdown {
			stop()
		}
		_ = bundle.sqlDB.Close()
	}
}

// buildChiMux composes the flat-stack routes. Pure constructor (no ports,
// no goroutines) so contract tests cover the exact composition production
// serves.
func buildChiMux(
	a *auth.Auth, todoH *chi.Handler,
	landingH *landingchi.Handler, configH *configchi.Handler,
	wbH *wbchi.Handler, notesH *notesachi.Handler,
	roomH *roomchi.Handler, chatH *chatchi.Handler, genuiH *genuichi.Handler,
	creditsH *creditschi.Handler,
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
	if roomH != nil {
		mux.Handle("/room", roomH.Routes())
		mux.Handle("/room/", roomH.Routes())
	}
	mux.Handle("/chat", chatH.Routes())
	mux.Handle("/chat/", chatH.Routes())
	mux.Handle("/api/chat/", chatH.Routes())
	mux.Handle("/genui", genuiH.Routes())
	mux.Handle("/genui/", genuiH.Routes())
	mux.Handle("/api/genui/", genuiH.Routes())
	mux.Handle("/api/credits", creditsH.Routes())
	mux.Handle("/api/credits/", creditsH.Routes())
	mux.Handle("/api/ai/", creditsH.Routes())
	mux.Handle("/api/byok/", creditsH.Routes())
	mux.Handle("/", landingH.Routes())
	return mux
}
