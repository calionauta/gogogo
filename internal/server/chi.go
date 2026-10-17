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
	"path/filepath"
	"time"

	"github.com/calionauta/gogogo/config"
	chathandlers "github.com/calionauta/gogogo/features/chat/handlers"
	confighandlers "github.com/calionauta/gogogo/features/config/handlers"
	"github.com/calionauta/gogogo/features/credits"
	creditshandlers "github.com/calionauta/gogogo/features/credits/handlers"
	genuihandlers "github.com/calionauta/gogogo/features/genui/handlers"
	landinghandlers "github.com/calionauta/gogogo/features/landing/handlers"
	"github.com/calionauta/gogogo/features/notes"
	noteshandlers "github.com/calionauta/gogogo/features/notes/handlers"
	"github.com/calionauta/gogogo/features/onboarding"
	reviewhandlers "github.com/calionauta/gogogo/features/review/handlers"
	roomhandlers "github.com/calionauta/gogogo/features/room/handlers"
	todohandlers "github.com/calionauta/gogogo/features/todo/handlers"
	"github.com/calionauta/gogogo/features/whiteboard"
	wbhandlers "github.com/calionauta/gogogo/features/whiteboard/handlers"
	"github.com/calionauta/gogogo/internal/auth"
	"github.com/calionauta/gogogo/internal/collab"
	"github.com/calionauta/gogogo/internal/db"
	dbgen "github.com/calionauta/gogogo/internal/db/sqlcgen"
	"github.com/calionauta/gogogo/internal/dbschema"
	appgoakt "github.com/calionauta/gogogo/internal/goakt"
	"github.com/calionauta/gogogo/internal/nats"
	"github.com/calionauta/gogogo/internal/natswire"
	"github.com/calionauta/gogogo/internal/queue"
	"github.com/calionauta/gogogo/web/resources"
)

// shutdownTimeout bounds chi-stack shutdown: in-flight SSE streams get a
// few seconds to finish before the process exits.
const shutdownTimeout = 5 * time.Second

// ChiStack is the booted flat stack, held apart so callers can attach
// JetStream and serve in the order their boot graph requires (see the file
// header). The zero value is not usable; build with BootChi.
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
	TodoHandler *todohandlers.Handler
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
) (*roomhandlers.Handler, *appgoakt.Host) {
	if !cfg.GoAkt.Enabled {
		return nil, nil
	}
	host := appgoakt.New()
	if herr := host.Start(ctx); herr != nil {
		slog.Warn("server: goakt failed to start, room demo disabled", "error", herr)
		return nil, nil
	}
	appgoakt.SetDefault(host)
	return roomhandlers.NewHandler(cfg, a, host), host
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
// create path resumes the paused run through the resumer interface.
// Returns nil (no shutdown hooks) when the engine is disabled, so a stock
// boot never registers the route and the resume hook stays off. Split from
// BootChi to keep boot under the length gate, mirroring bootCredits.
func bootOnboarding(
	cfg *config.Config, a *auth.Auth, todoH *todohandlers.Handler, hub *queue.SSEHub,
) (*onboarding.Handler, []func()) {
	if !cfg.DagNats.Enabled {
		return nil, nil
	}
	h := onboarding.NewHandler(a, cfg.DagNats.HTTPAddr, hub)
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
) (*creditshandlers.Handler, []func(), error) {
	ledgerDB, lerr := creditshandlers.OpenTursoLedger(cfg.DataDir)
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
		return creditshandlers.NewHandler(a, nil), shutdown, nil
	}
	creditsSvc.Start(ctx)
	return creditshandlers.NewHandler(a, creditsSvc), shutdown, nil
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
	if merr := dbschema.Migrate(sqlDB); merr != nil {
		_ = sqlDB.Close()
		return nil, fmt.Errorf("server: chi db migrate: %w", merr)
	}

	a, aerr := auth.New(dbgen.New(sqlDB), !cfg.Dev)
	if aerr != nil {
		_ = sqlDB.Close()
		return nil, fmt.Errorf("server: chi auth: %w", aerr)
	}
	// Fresh-install contract: the demo accounts the login form advertises,
	// including their roles (demo1 admin, demo2 viewer). Idempotent, so
	// every boot after the first is a role refresh.
	queries := dbgen.New(sqlDB)
	seedDemoAccounts(a, queries)
	// At-rest TOTP encryption is opt-in by env (AUTH_ENC_KEY): a malformed
	// key fails the boot rather than running half-encrypted.
	if kerr := wireTOTPKey(a, cfg); kerr != nil {
		_ = sqlDB.Close()
		return nil, kerr
	}

	bootCtx, cancelConsumer := context.WithCancel(context.Background())
	// Retained (not passed anywhere here): the engine boots below
	// (DagNats, GoAkt, credits) take bootCtx, and their trims drop those
	// lines — combined trims would otherwise leave bootCtx unused, which
	// does not compile. One explicit use keeps every composition green.
	_ = bootCtx

	// Chi-owned queue: feature workers run here so they never compete
	// with the other stack's workers for the same jobs. Its hub is the
	// SINGLE hub every stream and every worker shares: workers broadcast
	// to chiQ.Hub(), so every handler below is built with that same hub.
	// A second hub object silently swallows all worker feedback (the
	// Queue + Retry demo stuck on step 1 was exactly this: the todo
	// stream listened on a standalone hub while workers broadcast on
	// the queue's own hub).
	chiQ, qerr := queue.NewAt(filepath.Join(cfg.DataDir, "chi"))
	if qerr != nil {
		return bootFail(cancelConsumer, sqlDB, "queue", qerr)
	}
	hub := chiQ.Hub()
	todoH := todohandlers.NewHandler(queries, a, hub, cfg.Skin)
	wireTodoDisplay(todoH, cfg)
	if serr := bootSearch(cfg, todoH, sqlDB); serr != nil {
		return bootFail(cancelConsumer, sqlDB, "search", serr)
	}

	// Shutdown hooks stop independent things: stepper, actor, queue,
	// ledger. The order is arbitrary.
	shutdown := []func(){chiQ.Close}

	// Onboarding wires before the actor boot so its hook lands first.
	onboardingH, onbShutdown := bootOnboarding(cfg, a, todoH, hub)
	shutdown = append(shutdown, onbShutdown...)

	roomH, roomHost := bootRoomActor(bootCtx, cfg, a)
	if roomHost != nil {
		shutdown = append(shutdown, func() { stopRoomActor(roomHost) })
	}

	realLLM, simulatedLLM := wireTodoLLM(todoH, chiQ, cfg)
	// Retained for the trimmed tree: the genui trim drops the constructions
	// below, and an unused local would not compile there.
	_, _ = realLLM, simulatedLLM
	chatH, genuiH, err := bootAI(cfg, a, chiQ, hub, realLLM, simulatedLLM, queries)
	if err != nil {
		cancelConsumer()
		return nil, err
	}
	reviewH := bootReview(cfg, a, realLLM)
	chiQ.StartWorkers()

	creditsH, shutdown, err := bootCredits(bootCtx, cfg, shutdown, sqlDB, cancelConsumer, a)
	if err != nil {
		return nil, err
	}

	persister, perr := collab.SnapshotStore(cfg.Collab.Snapshots, queries)
	if perr != nil {
		return bootFail(cancelConsumer, sqlDB, "collab snapshots", perr)
	}
	docs := collab.NewDocStore()
	docs.SetPersister(persister)
	// One argument per line, deliberately: every line belongs to exactly one
	// feature, so trimming a feature drops its lines without touching its
	// neighbours. A shared line would make a line drop take two features at
	// once (and the add path restores these same lines — see routerCallLines).
	mux := buildChiMux(a, todoH,
		landinghandlers.NewHandler(cfg, a),
		confighandlers.NewHandler(cfg, a, queries),
		wbhandlers.NewHandler(queries, a, hub, persister, docs, cfg),
		noteshandlers.NewHandler(queries, a, hub, persister, docs, cfg),
		roomH,
		chatH,
		genuiH,
		reviewH,
		creditsH,
	)
	mountOnboarding(mux, onboardingH)
	// Stream liveness for verified SSE open (see StreamStatusHandler):
	// mounted here, not in buildChiMux, because only BootChi holds the
	// shared hub and the mount-after-composition rule keeps feature lines
	// droppable. Public like /health; longest match wins over the landing
	// catch-all regardless of registration order.
	mux.HandleFunc("/api/stream/status", StreamStatusHandler(hub))
	// Console rides the same mount-after-composition rule: the engine
	// listener is loopback-only, so browsers reach it only through here.
	mountConsoleProxy(mux, a, cfg)
	// RBAC is wired, not optional plumbing: one enforcer built here and the
	// middleware wraps the whole mux (pass-through when disabled, enforced
	// denies when on). A parse failure is a programmer error and fails the
	// boot loud. Split out of BootChi for the funlen gate.
	handler, rerr := applyRBAC(bootCtx, a, cfg, queries, mux)
	if rerr != nil {
		return bootFail(cancelConsumer, sqlDB, "rbac", rerr)
	}
	return &ChiStack{
		sqlDB:          sqlDB,
		mux:            handler,
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
	startCrudConsumer(ctx, js, s.appName, s.TodoHandler, s.hub)
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

// mountOnboarding registers the onboarding routes after the composition,
// not inside it: buildChiMux takes one concrete pointer per feature, and
// every extra parameter is a line every trim must edit. The optional mount
// keeps the signature stable while the engine is present or not. Nil is a
// no-op, so a stock boot never registers the route.
func mountOnboarding(mux *http.ServeMux, onboardingH *onboarding.Handler) {
	if onboardingH != nil {
		mux.Handle("/api/onboarding", onboardingH.Routes())
		mux.Handle("/api/onboarding/", onboardingH.Routes())
	}
}

// buildChiMux composes the flat-stack routes. Pure constructor (no ports,
// no goroutines) so contract tests cover the exact composition production
// serves. The onboarding routes are NOT parameters here: they mount after
// the composition in BootChi, so adding or removing an optional surface
// never changes this signature (see the mount block there).
func buildChiMux(
	a *auth.Auth, todoH *todohandlers.Handler,
	landingH *landinghandlers.Handler,
	configH *confighandlers.Handler,
	wbH *wbhandlers.Handler,
	notesH *noteshandlers.Handler,
	roomH *roomhandlers.Handler,
	chatH *chathandlers.Handler,
	genuiH *genuihandlers.Handler,
	reviewH *reviewhandlers.Handler,
	creditsH *creditshandlers.Handler,
) *http.ServeMux {
	// Every sub-router serves absolute paths (the auth handler carries
	// its /api/auth base path; chi routers match full paths too), and
	// the std mux never strips prefixes — so each subtree pattern routes
	// to the handler owning those absolute routes. Longest match wins;
	// the landing root is the catch-all.
	mux := http.NewServeMux()
	mux.Handle("/api/auth/", a.Handler())
	// Session probe at the top level (see SessionProbeHandler: inside the
	// auth sub-mux this would prefix into /api/auth/api/session).
	mux.Handle("/api/session", a.SessionProbeHandler())
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
	mux.Handle("/review", reviewH.Routes())
	mux.Handle("/review/", reviewH.Routes())
	mux.Handle("/api/review/", reviewH.Routes())
	mux.Handle("/api/credits", creditsH.Routes())
	mux.Handle("/api/credits/", creditsH.Routes())
	mux.Handle("/api/ai/", creditsH.Routes())
	mux.Handle("/api/byok/", creditsH.Routes())
	// Healthcheck: the scratch image runs `app health`, which GETs this
	// path on 127.0.0.1:PORT. Register before the landing catch-all so
	// the exact path wins.
	// Core bundle + Service Worker (see static.go).
	mountStatic(mux)

	// Feature packages embed their OWN assets (whiteboard.js + rough.min.js,
	// notes.js) — not in the core bundle, so each mounts its own subtree and
	// the longest match wins. These lines stay here on purpose: a feature
	// trim drops exactly its line, and the add path restores it from the
	// same list.
	mux.Handle("/static/wb/", resources.AssetHandler(whiteboard.StaticFS(), "/static/wb"))
	mux.Handle("/static/notes/", resources.AssetHandler(notes.StaticFS(), "/static/notes"))

	mux.HandleFunc("/health", func(w http.ResponseWriter, _ *http.Request) {
		// The build tag is what a tab open across a deploy compares
		// against (stale-tab reload). The body stays "ok" — the scratch image's
		// healthcheck and the Playwright smoke both match on it.
		// no-store: an intermediary caching this response would serve a
		// stale tag and fake a mismatch the other way (see session.js
		// buildCheck and the smoke assertion below).
		w.Header().Set("Cache-Control", "no-store")
		w.Header().Set("X-Gogogo-Build", todoH.BuildLabel+"-"+todoH.BuildCommit)
		w.WriteHeader(http.StatusOK)
		_, _ = w.Write([]byte("ok"))
	})
	mux.Handle("/", landingH.Routes())
	return mux
}
