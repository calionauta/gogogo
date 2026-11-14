// Flat-stack chi server (the only server)
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
//
// Routing is chi end to end: the root is a chi.Router, each capability's own
// router is flattened onto it (see internal/server/route.go), and a method
// the route tree does not know answers 405 instead of a hand-rolled check
// inside the handler.
package server

import (
	"context"
	"database/sql"
	"fmt"
	"log/slog"
	"net"
	"net/http"
	"os"
	"time"

	natsio "github.com/nats-io/nats.go"

	"github.com/calionauta/gogogo/config"
	"github.com/calionauta/gogogo/features/notes"
	"github.com/calionauta/gogogo/features/notifications"
	"github.com/calionauta/gogogo/features/onboarding"
	"github.com/calionauta/gogogo/features/whiteboard"
	"github.com/calionauta/gogogo/internal/auth"
	"github.com/calionauta/gogogo/internal/collab"
	"github.com/calionauta/gogogo/internal/db"
	dbgen "github.com/calionauta/gogogo/internal/db/sqlcgen"
	"github.com/calionauta/gogogo/internal/dbschema"
	appgoakt "github.com/calionauta/gogogo/internal/goakt"
	"github.com/calionauta/gogogo/internal/instance"
	"github.com/calionauta/gogogo/internal/llm"
	"github.com/calionauta/gogogo/internal/nats"
	"github.com/calionauta/gogogo/internal/natswire"
	"github.com/calionauta/gogogo/internal/queue"
	"github.com/calionauta/gogogo/internal/recipe"
	"github.com/calionauta/gogogo/internal/recipe/register"
)

// shutdownTimeout bounds chi-stack shutdown: in-flight SSE streams get a
// few seconds to finish before the process exits.
const shutdownTimeout = 5 * time.Second

// TodoCreator is what the DagNats onboarding worker needs from the todo
// slice (one method, so cmd/web stays decoupled from the handlers package
// the root no longer imports).
type TodoCreator interface {
	CreateTodoForOnboarding(ctx context.Context, title, owner string) error
}

// NotificationsDeliverer is what the DagNats reminder worker needs from
// the inbox slice (one deliver method, in features/notifications so the
// root never imports handlers).
type NotificationsDeliverer notifications.Deliverer

// todoHandle adapts the Register-published funcs to TodoCreator.
type todoHandle struct{ ref *register.TodoRef }

func (h todoHandle) CreateTodoForOnboarding(ctx context.Context, title, owner string) error {
	if h.ref == nil || h.ref.CreateTodoForOnboarding == nil {
		return nil
	}
	return h.ref.CreateTodoForOnboarding(ctx, title, owner)
}

// inboxHandle adapts InboxRef to NotificationsDeliverer.
type inboxHandle struct{ ref *register.InboxRef }

func (h *inboxHandle) Deliver(ctx context.Context, d notifications.Delivery) error {
	if h == nil || h.ref == nil || h.ref.Deliver == nil {
		return nil
	}
	return h.ref.Deliver(ctx, d)
}

func (h *inboxHandle) SetRunStarter(r notifications.RunStarter) {
	if h == nil || h.ref == nil || h.ref.SetRunStarter == nil {
		return
	}
	h.ref.SetRunStarter(r)
}

func (h *inboxHandle) Wired() bool {
	if h == nil || h.ref == nil || h.ref.Wired == nil {
		return false
	}
	return h.ref.Wired()
}

// ChiStack is the booted flat stack, held apart so callers can attach
// JetStream and serve in the order their boot graph requires (see the file
// header). The zero value is not usable; build with BootChi.
type ChiStack struct {
	sqlDB          *sql.DB
	mux            http.Handler
	cancelConsumer context.CancelFunc
	// shutdown runs on stop, after the HTTP server drains (same cleanup as Serve's stop func).
	shutdown []func()

	appName string
	queries *dbgen.Queries
	hub     *queue.SSEHub
	// TodoHandler is the todo slice handle engines wire against: the
	// DagNats onboarding worker creates example todos through it, so
	// cmd/web passes this to startDagNats before NATS exists. Its type
	// is the one-method interface above, filled from the Register
	// bridge so the root never imports the handlers package.
	TodoHandler TodoCreator
	// todoRef is the full Register bridge (publisher + consumer) the
	// AttachJetStream step needs; nil when the todo recipe mounted
	// nothing (never on a stock tree).
	todoRef *register.TodoRef
	// NotificationsHandler is the delivery side engines wire against: the
	// DagNats reminder worker writes the notification row through it, so
	// cmd/web passes this to startDagNats before NATS exists. Filled from
	// InboxRef so the root never imports the handlers package.
	NotificationsHandler NotificationsDeliverer
	// inboxRef is the full Register bridge (bus) the shell feed needs.
	inboxRef *register.InboxRef
	// presence is the roster seam both collaborative features share (created
	// at boot, federated by AttachPresenceFederation). Nil = features trimmed.
	presence *collab.FederatedPresence
}

// bootOnboarding wires the DagNats onboarding slice onto the chi stack:
// its stepper publishes to the chi hub, its route is served, and the todo
// create path resumes the paused run through the resumer interface.
// Returns nil (no shutdown hooks) when the engine is disabled, so a stock
// boot never registers the route and the resume hook stays off. Split from
// BootChi to keep boot under the length gate, mirroring bootCredits.
func bootOnboarding(
	cfg *config.Config, a *auth.Auth, ref *register.TodoRef, hub *queue.SSEHub,
) (*onboarding.Handler, []func()) {
	if !cfg.DagNats.Enabled {
		return nil, nil
	}
	h := onboarding.NewHandler(a, cfg.DagNats.HTTPAddr, hub)
	if ref != nil && ref.SetOnboarding != nil {
		ref.SetOnboarding(h)
	}
	return h, []func(){h.Shutdown}
}

// BootChi boots the flat stack without listening and without JetStream:
// the returned stack serves its mux and has cross-instance sync off until
// AttachJetStream runs. js-free by design — the engine owns the NATS, so
// JetStream is attached after boot, never passed into it.
//
//nolint:funlen // composition root: one line per feature, and the installer
func BootChi(cfg *config.Config) (*ChiStack, error) {
	if verr := cfg.ValidateModes(); verr != nil {
		return nil, verr
	}
	sqlDB, err := db.Open(cfg)
	if err != nil {
		return nil, fmt.Errorf("server: chi db open: %w", err)
	}
	if merr := dbschema.Migrate(sqlDB); merr != nil {
		_ = sqlDB.Close()
		return nil, fmt.Errorf("server: chi db migrate: %w", merr)
	}
	// First-run door: production defaults close signup, so an empty database
	// would have no way to create user #1. Runs before wireAuth reads the flag.
	bootstrapSignup(cfg, dbgen.New(sqlDB))
	// Cross-instance identity: names the CRUD durable and tags every
	// published event so an instance ignores its own echo. Derived (hostname)
	// unless APP_INSTANCE_ID pins it.
	slog.Info("server: instance identity", "id", instance.ID())

	a, aerr := auth.New(dbgen.New(sqlDB), cfg.Auth.SecureCookies)
	if aerr != nil {
		_ = sqlDB.Close()
		return nil, fmt.Errorf("server: chi auth: %w", aerr)
	}
	if werr := wireAuth(a, cfg); werr != nil {
		_ = sqlDB.Close()
		return nil, fmt.Errorf("server: chi auth wiring: %w", werr)
	}
	// Fresh-install contract: the demo accounts the login form advertises,
	// including their roles (demo1 admin, demo2 viewer). Idempotent, so
	// every boot after the first is a role refresh.
	queries := demoQueries(cfg, a, sqlDB)
	bootCtx, cancelConsumer := context.WithCancel(context.Background())
	startRetention(bootCtx, queries)
	// bootCtx owns the retention ticker and engines below; cancellation stops them.

	// Chi-owned queue: feature workers run here so they never compete
	// with the other stack's workers. Its hub is the SINGLE hub every
	// stream and worker shares (a second hub silently swallows feedback).
	chiQ, hub, qerr := newChiQueue(cfg)
	if qerr != nil {
		return bootFail(cancelConsumer, sqlDB, "queue", qerr)
	}
	// Shutdown hooks stop independent things; onShutdown is handed to
	// Deps so self-mounting units register theirs without type knowledge.
	shutdown := []func(){chiQ.Close}
	onShutdown := func(f func()) { shutdown = append(shutdown, f) }

	// Todo handle holder: the todo Register fills it during bootRecipes
	// so engines (JetStream, onboarding, DagNats worker) wire without the
	// root importing the handlers package.
	todoRef := &register.TodoRef{}
	inboxRef := &register.InboxRef{}
	chatRef := &register.ChatRef{}
	creditsRef := &register.CreditsRef{}
	panels := []any{}

	// LLM clients are built once here and shared by the todo and chat
	// Registers (same clients BootChi used to hand each handler).
	realLLM := llm.New(cfg.GoAI.APIKey)
	var simulatedLLM *llm.Client
	if os.Getenv("SIMULATE_LLM") != "false" {
		simulatedLLM = llm.NewSimulated()
	}
	// One slot registry for the boot: the AI panel renders the SAME row.
	slots := recipe.NewSlotRegistry()
	chiQ.StartWorkers()

	persister, docs, perr := bootCollab(cfg, queries)
	if perr != nil {
		return bootFail(cancelConsumer, sqlDB, "collab snapshots", perr)
	}
	// Collaborator presence: ONE local seam for both features (the actor when
	// GoAkt runs, the in-process PeerSet otherwise), so a deployment cannot end
	// up with two rosters that disagree about the same tab. It is federated
	// across instances in AttachPresenceFederation once NATS exists.
	federated := bootPresence(hub)
	// One argument per line: a trim drops its features' lines, nothing else.
	mux := buildChiMux(a,
		register.Deps{
			Cfg: cfg, Auth: a, Queries: queries, Hub: hub, Queue: chiQ,
			RealLLM: realLLM, SimulatedLLM: simulatedLLM,
			OnShutdown: onShutdown, Slots: slots, SQLDB: sqlDB,
			Persister: persister, DocStore: docs, Presence: federated,
			TodoRef: todoRef, InboxRef: inboxRef,
			ChatRef: chatRef, CreditsRef: creditsRef,
			Panels: &panels,
		},
	)
	if mux.recipeErr != nil {
		return bootFail(cancelConsumer, sqlDB, "recipes", mux.recipeErr)
	}
	if perr := installPanels(panels); perr != nil {
		return bootFail(cancelConsumer, sqlDB, "panels", perr)
	}
	if herr := healSearch(bootCtx, todoRef); herr != nil {
		return bootFail(cancelConsumer, sqlDB, "search", herr)
	}
	// Onboarding wires after the recipe step: TodoRef.SetOnboarding is
	// filled by the todo Register, so the resumer lands on the handler the
	// seam mounted.
	onboardingH, onbShutdown := bootOnboarding(cfg, a, todoRef, hub)
	shutdown = append(shutdown, onbShutdown...)
	// roomHost is read by the graph mount and the presence upgrade from
	// the process default (room Register set it during bootRecipes above);
	// nil when the room recipe registered nothing (GoAkt off or host start
	// failed). The upgrade mutates the shared federated object the two
	// collab handlers already hold, so ordering after buildChiMux is correct.
	roomHost := appgoakt.Default()
	// trim:presence-actor
	if roomHost != nil {
		upgradePresenceToActor(federated, appgoakt.NewDocPresence(roomHost))
	}
	mountOnboarding(mux, onboardingH)
	// Distribution axes (open auth + activation gate). Mounted here because
	// the activation feature is a normal router mount; the open-auth owner is
	// installed before any request can arrive (Serve happens after BootChi).
	licStore, modeErr := bootModes(cfg, a, queries, mux)
	if modeErr != nil {
		return bootFail(cancelConsumer, sqlDB, "access modes", modeErr)
	}
	// Console rides the same mount-after-composition rule: the engine listener
	// is loopback-only, so browsers reach it only through here.
	mountConsoleProxy(mux, a, cfg)
	mountGraph(mux, a, cfg, roomHost)
	// Runtime dashboard: opt-in, operator-only, mounted before applyRBAC (internal/server/perf.go).
	perfH := bootPerf(mux, a, cfg, &shutdown)
	// Inbox bus feed (the inbox itself mounted via the seam).
	mountInbox(a, queries, inboxRef)
	notificationsH := &inboxHandle{ref: inboxRef}

	// RBAC is wired, not optional plumbing: one enforcer built here and the
	// middleware wraps the whole mux (pass-through when disabled, enforced
	// denies when on). A parse failure is a programmer error and fails the
	// boot loud. Split out of BootChi for the funlen gate.
	handler, rerr := applyRBAC(bootCtx, a, cfg, queries, mux)
	if rerr != nil {
		closePerf(perfH)
		return bootFail(cancelConsumer, sqlDB, "rbac", rerr)
	}
	mux.invokeOut = handler // invoke re-serves THROUGH this handler (see extinvoke.go)
	// The edge wrappers sit above RBAC on purpose: a 403 and a router 404 carry the same headers.
	handler = rateLimit(cfg.RateLimitRPM, shellMiddleware(cfg, a, queries, notificationsH, shellNav(slots), handler))
	// Inside the edge wrappers (headers/log still apply) and outside the router: every route is gated.
	handler = applyLicenseGate(cfg, licStore, handler)
	handler = applyI18N(cfg, handler)
	handler = edgeHandler(cfg, slog.Default(), handler)
	return &ChiStack{
		sqlDB:          sqlDB,
		mux:            handler,
		cancelConsumer: cancelConsumer,
		shutdown:       shutdown,
		appName:        cfg.AppName,
		queries:        queries,
		hub:            hub,
		TodoHandler:    todoHandle{ref: todoRef},
		todoRef:        todoRef,
		// NotificationsHandler is the delivery side engines wire against: the
		// DagNats reminder worker writes the notification row through it, so
		// cmd/web passes this to startDagNats before NATS exists.
		NotificationsHandler: notificationsH,
		inboxRef:             inboxRef,
		presence:             federated,
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
func (s *ChiStack) AttachJetStream(
	ctx context.Context, nc *natsio.Conn, js nats.JetStreamLike, leafEdge bool,
) {
	pub := natswire.NewCrudPublisher(js, instance.ID())
	if leafEdge {
		// A leaf edge forwards its live ops over core NATS; its own
		// JetStream stream stays in the local domain.
		pub.SetCore(nc)
	}
	slog.Info("server: crud wire attached", "leafEdge", leafEdge, "publisher", pub != nil)
	if s.todoRef != nil && s.todoRef.SetPublisher != nil {
		s.todoRef.SetPublisher(pub)
	}
	if s.todoRef != nil && s.todoRef.RunCrudConsumer != nil {
		run := s.todoRef.RunCrudConsumer
		go func() {
			if err := run(ctx, js, s.appName, instance.ID()); err != nil {
				slog.Error("server: crud consumer stopped", "error", err)
			}
		}()
		return
	}
	// Fallback for callers that built the stack without a TodoRef (tests).
	slog.Warn("server: crud wire without todo bridge, consumer skipped")
}

// srvHandler wraps the mux for serveChiOnListener (kept separate so the
// listener stays the caller's: tests bind :0 and read the port back).
//
// Timeouts: ReadHeaderTimeout bounds slow headers, ReadTimeout bounds a slow
// body (JSON/Loro payloads are small; 60s is generous for an edge link), and
// IdleTimeout releases abandoned keep-alives. WriteTimeout stays 0 on
// purpose: a finite value is a transport deadline, and every SSE route here
// is a response that never ends — the first write after the deadline would
// cut the stream. MaxHeaderBytes halves the 1 MiB default; cookies and
// forwarding headers are the only large ones.
func srvHandler(mux http.Handler) *http.Server {
	return &http.Server{
		Handler:           mux,
		ReadHeaderTimeout: 10 * time.Second,
		ReadTimeout:       60 * time.Second,
		IdleTimeout:       120 * time.Second,
		WriteTimeout:      0,
		MaxHeaderBytes:    64 << 10,
	}
}

// Serve binds addr and serves the stack; the returned func stops it
// (consumer cancelled, HTTP drained, shutdown hooks, DB closed). The
// listener binds synchronously so a taken port fails at the call site.
// bound is the address the listener actually holds — a :0 request reports
// the kernel-chosen port, so a caller (the desktop window) knows where to
// point. Callers defer the stop func.
func (s *ChiStack) Serve(addr string) (stop func(), bound string, err error) {
	ln, err := (&net.ListenConfig{}).Listen(context.Background(), "tcp", addr)
	if err != nil {
		s.cancelConsumer()
		_ = s.sqlDB.Close()
		return nil, "", fmt.Errorf("server: chi listen %s: %w", addr, err)
	}
	return serveChiOnListener(ln, srvHandler(s.mux), s), ln.Addr().String(), nil
}

// StartChi boots, attaches JetStream and serves in one call — the
// single-function entry for frontends whose boot order allows it
// (cmd/desktop: NATS is up before the stack boots). A nil stop with a nil
// error means "seam disabled" (no ChiAddr), so a stock env never changes
// behaviour. addr is the address the listener holds after a :0 request.
// cmd/web uses Boot/Attach/Serve directly: its DagNats engine owns the NATS
// and must boot first.
func StartChi(cfg *config.Config, nc *natsio.Conn, js nats.JetStreamLike) (stop func(), addr string, err error) {
	if cfg.ChiAddr == "" {
		return nil, "", nil
	}
	stack, err := BootChi(cfg)
	if err != nil {
		return nil, "", err
	}
	stack.AttachJetStream(context.Background(), nc, js, cfg.NATS.LeafNodeURL != "")
	if aerr := stack.AttachEphemeral(nc); aerr != nil {
		slog.Warn("server: ephemeral bridge not attached", "error", aerr)
	}
	if derr := stack.AttachDocBridge(nc); derr != nil {
		slog.Warn("server: doc bridge not attached", "error", derr)
	}
	return stack.Serve(cfg.ChiAddr)
}

// mountOnboarding registers the onboarding routes after the composition, not
// inside it: buildChiMux takes one concrete pointer per feature, and every extra
// parameter is a line every trim must edit. Nil is a no-op, so a stock boot
// never registers the route.
func mountOnboarding(r *Router, onboardingH *onboarding.Handler) {
	if onboardingH != nil {
		r.MountFeature("onboarding", onboardingH.Routes())
	}
}

// buildChiMux composes the flat-stack routes. Pure constructor (no ports, no
// goroutines) so contract tests cover the exact composition production serves.
// Optional surfaces mount after it, so their absence never changes this
// signature (see the mount block in BootChi).
//
// Convention: a feature router declares its own absolute paths, and
// MountFeature flattens that table onto the root (internal/server/route.go) —
// one call per capability, no route list at the root to drift out of sync.
// The landing router is also the not-found handler, so an unknown path lands
// on the same page every unmatched URL already has.
func buildChiMux(
	a *auth.Auth, deps register.Deps,
) *Router {
	r := NewRouter()
	// A panic in a handler answers 500 instead of dropping the connection and
	// taking an SSE stream down with it.
	r.Use(recoveryMiddleware)

	// Auth is a flat feature like every other surface: its router owns the
	// absolute paths, the methods (405 at the router) and the OAuth provider
	// parameter, so the route table claims each endpoint.
	r.MountFeature("auth", a.Routes())
	// The session stream is its own path, not a child of /api/auth: it is a
	// long-lived SSE the banner holds open and it only hears about changes.
	MountPath(r, "/api/session/stream", a.SessionStreamHandler())

	// Installed recipes mount through the seam.
	r.recipes = &recipe.Registry{}
	// Units that mount themselves (Capability.Register) are registered by the
	// generated list in units.gen.go, not constructed here.
	// Installed recipes register through the generated file, then their own
	// migrations run, then they mount: one step, in that order (recipe_surface.go).
	r.recipeErr = bootRecipes(r, deps)

	// Static: the core bundle, then the Service Worker at the ROOT URL so its
	// scope is "/" (see static.go). HEAD rides GET per HTTP semantics.
	MountSubtree(r, "/static", staticHandler())
	// Demo mode: a session cookie that docks the state panel. Public on purpose —
	// it sets a display preference, and the landing page has no panel to dock.
	r.Claim("core", "GET", "/_ui/demo")
	r.Get("/_ui/demo", demoModeHandler)
	registerLocaleRoute(r, deps.Cfg)
	r.Claim("core", "GET", "/sw.js")
	r.Claim("core", "HEAD", "/sw.js")
	r.Get("/sw.js", serviceWorkerHandler().ServeHTTP)
	r.Head("/sw.js", serviceWorkerHandler().ServeHTTP)
	// Features that embed their OWN assets (whiteboard.js + rough.min.js,
	// notes.js) register their subtree here; the core /static/ mount never
	// serves them. A feature trim drops its line and its import.
	MountSubtree(r, whiteboard.AssetPath, whiteboard.StaticHandler())
	MountSubtree(r, notes.AssetPath, notes.StaticHandler())

	// Healthcheck: the scratch image runs `app health`, which GETs this path
	// on 127.0.0.1:PORT. HEAD rides GET per HTTP semantics; any other method
	// is a 405 from the tree.
	r.Claim("core", "POST", cspReportPath)
	r.Post(cspReportPath, cspReportHandler)
	mountExtInvoke(r)
	// The root is core: a redirect to the app's home (or /login), not a page.
	mountHome(r, a)
	r.Claim("core", "GET", "/health")
	r.Claim("core", "HEAD", "/health")
	r.Get("/health", healthHandler(deps.Cfg))
	r.Head("/health", healthHandler(deps.Cfg))
	return r
}
