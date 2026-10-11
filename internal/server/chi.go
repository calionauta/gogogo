// SCOPE:core — Flat-stack chi server (the only server)
//
// BootChi / AttachJetStream / Serve compose the flat stack: boot builds
// everything that needs no engines (DB, auth, handlers, hubs, queues,
// workers), AttachJetStream installs the cross-instance publisher and
// consumer once the DagNats-owned NATS is reachable, and Serve binds the
// listener. cmd/web needs the split: DagNats owns the embedded NATS and
// must boot before JetStream exists, and the onboarding worker needs the
// todo handler BootChi returns — one function could not have both.
// StartChi keeps the single-call shape for frontends whose boot order
// allows it (cmd/desktop). ChiAddr empty disables the serve path, so a
// stock env never changes behaviour.
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

// ChiStack is the booted flat stack, held apart so callers can attach
// JetStream and serve in the order their boot graph requires. Boot owns
// DB, auth, handlers, hubs, onboarding, credits, queues and workers —
// everything that needs no engines. AttachJetStream installs the
// cross-instance publisher + consumer, which need the NATS the DagNats
// engine owns and therefore cannot exist at boot time. Serve binds the
// listener. The zero value is not usable; build with BootChi.
type ChiStack struct {
	sqlDB          *sql.DB
	mux            http.Handler
	cancelConsumer context.CancelFunc
	// shutdown runs on stop, after the HTTP server drains: queue close,
	// actor halt. Same cleanup reachable through Serve's stop func.
	shutdown []func()

	appName string
	queries *dbgen.Queries
	hub     *queue.SSEHub
	// TodoHandler is the todo slice handle engines wire against: the
	// DagNats onboarding worker creates example todos through it, so
	// cmd/web passes this to startDagNats before NATS exists.
	TodoHandler *chi.Handler
}

// startCrudConsumer runs the JetStream CRUD consumer for cross-instance
// convergence until the boot context is cancelled. It is nil-safe on js
// (RunCrudConsumer no-ops when NATS is disabled). Called from
// AttachJetStream so it stays under boot's function-length gate.
func startCrudConsumer(
	ctx context.Context, js nats.JetStreamLike, appName string,
	q *dbgen.Queries, hub *queue.SSEHub,
) {
	go func() {
		if cerr := chi.RunCrudConsumer(ctx, js, appName, q, hub); cerr != nil {
			slog.Error("server: chi crud consumer stopped", "error", cerr)
		}
	}()
}

// bootRoomActor starts the GoAkt actor system and returns the room grains
// handler plus the host (nil, nil when disabled or when the engine failed
// to start: /room then degrades to 502s, same as the previous stack with
// its engine down — never to a failed boot). The shutdown hook is
// registered by the caller so the Background timeout in stopRoomActor
// lives outside a function carrying the boot ctx (that is what the
// contextcheck rule wants, and it keeps the hook's lifetime independent).
func bootRoomActor(
	ctx context.Context, cfg *config.Config, a *auth.Auth,
) (*roomchi.Handler, *appgoakt.Host) {
	if !cfg.GoAkt.Enabled {
		return nil, nil
	}
	host := appgoakt.New()
	if herr := host.Start(ctx); herr != nil {
		slog.Warn("server: goakt failed to start, room demo disabled", "error", herr)
		return nil, nil
	}
	appgoakt.SetDefault(host)
	return roomchi.NewHandler(cfg, a, host), host
}

// stopRoomActor is the shutdown hook for the GoAkt host. Its timeout
// starts from Background on purpose: the hook runs during stop(), while
// the boot context may already be cancelled, and a cancelled parent would
// make host.Stop fail instantly instead of draining grains.
func stopRoomActor(host *appgoakt.Host) {
	ctx, cancel := context.WithTimeout(context.Background(), shutdownTimeout)
	defer cancel()
	_ = host.Stop(ctx)
}

// bootOnboarding wires the DagNats onboarding slice onto the chi stack:
// its stepper publishes to the chi hub, its route is served, and the todo
// create path resumes the paused run. Returns nil (no shutdown hooks) when
// the engine is disabled, so a stock boot never registers the route and the
// resume hook stays off. Split from BootChi to keep boot under the length
// gate, mirroring bootCredits.
func bootOnboarding(
	cfg *config.Config, a *auth.Auth, todoH *chi.Handler, hub *queue.SSEHub,
) (*chi.OnboardingHandler, []func()) {
	if !cfg.DagNats.Enabled {
		return nil, nil
	}
	h := chi.NewOnboardingHandler(a, cfg.DagNats.HTTPAddr, hub)
	todoH.SetOnboarding(h)
	return h, []func(){h.Shutdown}
}

// bootCredits opens the Turso ledger, builds the engine (nil when the
// feature is disabled — routes answer 503), starts its workers and
// registers its shutdown. Split from BootChi to keep both under the
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

// BootChi boots the flat stack without listening and without JetStream:
// the returned stack serves its mux and has cross-instance sync off
// until AttachJetStream runs. js-free by design — the engine owns the
// NATS, so JetStream is attached after boot, never passed into it.
func BootChi(cfg *config.Config) (*ChiStack, error) {
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

	hub := queue.NewSSEHub()
	queries := dbgen.New(sqlDB)
	todoH := chi.NewHandler(queries, a, hub, cfg.Skin)
	todoH.BuildLabel = cfg.BuildLabel
	todoH.BuildCommit = cfg.BuildCommit
	todoH.OfflineSyncEnabled = cfg.OfflineSync.Enabled
	todoH.NATSEnabled = cfg.NATS.Enabled

	// Onboarding wires before the actor boot so the shutdown hooks are
	// preallocated from its slice. The hook order is arbitrary (they stop
	// independent things: stepper, actor, queue, ledger).
	onboardingH, onbShutdown := bootOnboarding(cfg, a, todoH, hub)
	shutdown := make([]func(), 0, 1+len(onbShutdown))
	shutdown = append(shutdown, onbShutdown...)

	roomH, roomHost := bootRoomActor(bootCtx, cfg, a)
	if roomHost != nil {
		shutdown = append(shutdown, func() { stopRoomActor(roomHost) })
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

	persister, perr := collab.NewTursoPersister(queries)
	if perr != nil {
		cancelConsumer()
		_ = sqlDB.Close()
		return nil, fmt.Errorf("server: chi snapshots: %w", perr)
	}
	docs := collab.NewDocStore()
	docs.SetPersister(persister)
	return &ChiStack{
		sqlDB: sqlDB,
		mux: buildChiMux(a, todoH,
			landingchi.NewHandler(cfg, a), configchi.NewHandler(cfg, a),
			wbchi.NewHandler(queries, a, hub, persister, docs, cfg),
			notesachi.NewHandler(queries, a, hub, persister, docs, cfg),
			roomH, chatH, genuiH, creditsH, onboardingH,
		),
		cancelConsumer: cancelConsumer,
		shutdown:       shutdown,
		appName:        cfg.AppName,
		queries:        queries,
		hub:            hub,
		TodoHandler:    todoH,
	}, nil
}

// AttachJetStream installs the cross-instance wiring on js: the CRUD
// publisher on the todo handler and the consumer goroutine, which runs
// until ctx is cancelled. Nil-safe by construction — NewCrudPublisher(nil)
// yields a nil publisher and RunCrudConsumer no-ops — so a NATS-off boot
// attaches nil and behaves exactly like the Hub-only stack. Call once.
//
// ctx is the caller's lifetime signal (cmd/web passes its lifecycleCtx;
// StartChi passes Background) — the stack holds no context of its own.
func (s *ChiStack) AttachJetStream(ctx context.Context, js nats.JetStreamLike) {
	s.TodoHandler.SetPublisher(natswire.NewCrudPublisher(js))
	startCrudConsumer(ctx, js, s.appName, s.queries, s.hub)
}

// Serve binds addr and serves the stack; the returned func stops it
// (consumer cancelled, HTTP drained, shutdown hooks, DB closed). The
// listener binds synchronously so a taken port fails at the call site.
// Callers defer the stop func.
func (s *ChiStack) Serve(addr string) (func(), error) {
	ln, err := (&net.ListenConfig{}).Listen(context.Background(), "tcp", addr)
	if err != nil {
		s.cancelConsumer()
		_ = s.sqlDB.Close()
		return nil, fmt.Errorf("server: chi listen %s: %w", addr, err)
	}
	return serveChiOnListener(ln, srvHandler(s.mux), s), nil
}

// StartChi boots, attaches JetStream and serves in one call — the
// single-function entry for frontends whose boot order allows it
// (cmd/desktop: NATS is up before the stack boots). A nil return with a
// nil error means "seam disabled" (no ChiAddr), so a stock env never
// changes behaviour. cmd/web uses Boot/Attach/Serve directly: its
// DagNats engine owns the NATS and must boot first.
func StartChi(cfg *config.Config, js nats.JetStreamLike) (stop func(), err error) {
	if cfg.ChiAddr == "" {
		return nil, nil
	}
	stack, err := BootChi(cfg)
	if err != nil {
		return nil, err
	}
	stack.AttachJetStream(context.Background(), js)
	return stack.Serve(cfg.ChiAddr)
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
func serveChiOnListener(ln net.Listener, srv *http.Server, stack *ChiStack) func() {
	go func() {
		if serr := srv.Serve(ln); serr != nil && serr != http.ErrServerClosed {
			slog.Error("server: chi stack stopped", "error", serr)
		}
	}()
	slog.Info("server: chi flat stack listening", "addr", ln.Addr())
	return func() {
		stack.cancelConsumer()
		ctx, cancel := context.WithTimeout(context.Background(), shutdownTimeout)
		defer cancel()
		_ = srv.Shutdown(ctx)
		for _, stop := range stack.shutdown {
			stop()
		}
		_ = stack.sqlDB.Close()
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
	creditsH *creditschi.Handler, onboardingH *chi.OnboardingHandler,
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
	if onboardingH != nil {
		mux.Handle("/api/onboarding", onboardingH.Routes())
		mux.Handle("/api/onboarding/", onboardingH.Routes())
	}
	mux.Handle("/api/ai/", creditsH.Routes())
	mux.Handle("/api/byok/", creditsH.Routes())
	// Healthcheck: the scratch image runs `app health`, which GETs this
	// path on 127.0.0.1:PORT. PocketBase served /health natively; the
	// flat stack owns it now. Register before the landing catch-all so
	// the exact path wins.
	mux.HandleFunc("/health", func(w http.ResponseWriter, _ *http.Request) {
		w.WriteHeader(http.StatusOK)
	})
	mux.Handle("/", landingH.Routes())
	return mux
}
