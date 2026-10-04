// SCOPE:layer=feature,removal=feature — gogpu/ui native window over the existing backend
//
// Command gui is a proof of concept: a native desktop window built with
// gogpu/ui (pure-Go GPU rendering, zero CGO, no webview, no JavaScript)
// that runs on the SAME backend as the web app — PocketBase, the demo
// user seed, and the EntityStore contract in features/store.
//
// What this proves, and what it does not:
//
//	Proved: the domain layer is genuinely transport-agnostic. Auth
//	  (features/auth.Login + ResolveOwner), persistence
//	  (store.EntityStore) and the todo model are consumed here with
//	  no *core.RequestEvent, no cookie, and no HTTP router. If the web
//	  app could be deleted and this app kept, the layering held.
//	  Cross-frontend visibility holds too: a todo created in the web
//	  app appears here within one poll tick (same collection).
//	Not proved: no offline sync (this window is ONLINE-ONLY — mutations
//	  run synchronously against the local DB and fail visibly when it
//	  is unreachable; there is no outbox, no replay, no Loro merge —
//	  see the whiteboard's whiteboard.js outbox for what that would
//	  take), no durable workflow trigger (handleCreate resumes the
//	  onboarding workflow; the native add() does not — the trigger
//	  lives in the HTTP layer and needs extracting to the store
//	  before both frontends inherit it), no multi-user push (polling
//	  only, no JetStream subscription), no theming parity with the
//	  web skins.
//
// Note there is no pb.Start() and no HTTP listener here. app.Bootstrap()
// opens the database without serving; the store reads and writes through
// the App handle directly. That is the proof in miniature — the web tier
// is genuinely optional for a native frontend.
//
// It is deliberately NOT part of `make build` / `make test`: it is a
// separate native target like cmd/desktop, validated by the gui-poc job
// in .github/workflows/desktop.yml (CGO_ENABLED=0, pure Go, so it builds
// headless anywhere `go` is; tests run headless too, no window/GPU).
//
// To remove: delete cmd/gui/, drop the `gui` target from the Makefile,
// drop the gui-poc job from desktop.yml, and re-add cmd/gui to
// scripts/web-packages.sh's exclusion... (it is currently excluded;
// removing the directory needs no script change).
package main

import (
	"fmt"
	"log"
	"log/slog"
	"sync"
	"time"

	_ "github.com/gogpu/gg/gpu" // enable GPU SDF acceleration
	"github.com/gogpu/gogpu"
	"github.com/gogpu/ui/app"
	"github.com/gogpu/ui/desktop"
	"github.com/gogpu/ui/theme/material3"
	"github.com/gogpu/ui/widget"
	"github.com/pocketbase/pocketbase"

	"github.com/calionauta/gogogo/config"
	"github.com/calionauta/gogogo/db"
	"github.com/calionauta/gogogo/features/auth"
	"github.com/calionauta/gogogo/features/store/pbstore"

	_ "github.com/ncruces/go-sqlite3/driver"
)

// todosCollection is the same PocketBase collection the web app writes.
// Reading the identical table is what makes this a second frontend
// rather than a parallel app.
const todosCollection = "todos"

// pollInterval is the realtime-read tick: how often the window
// re-reads the collection. No push channel exists in the POC, so this
// is what makes another frontend's writes visible here. 2s is the
// KISS point — fast enough to feel live, slow enough that a local
// SQLite re-list never shows up in profiles.
const pollInterval = 2 * time.Second

// Window chrome for the native POC.
const (
	seedColorHex = 0x6750A4
	windowWidth  = 520
	windowHeight = 560
)

func main() {
	if err := run(); err != nil {
		log.Fatal(err)
	}
}

func run() error {
	cfg := config.Load()

	pbApp, err := db.Init(cfg)
	if err != nil {
		return fmt.Errorf("pocketbase init: %w", err)
	}
	// Bootstrap (not Start) opens the database without serving HTTP.
	if err := pbApp.Bootstrap(); err != nil {
		return fmt.Errorf("pocketbase bootstrap: %w", err)
	}
	// Seed directly instead of relying on SeedDefaults' OnServe hook:
	// nothing is served here, so the hook would never fire and the
	// window would open on a database with no todos collection.
	if err := db.ApplySeeds(pbApp, cfg.OfflineSync.Enabled); err != nil {
		return fmt.Errorf("seed: %w", err)
	}

	m3 := material3.New(widget.Hex(seedColorHex))

	gogpuApp := gogpu.NewApp(gogpu.DefaultConfig().
		WithTitle("gogogo (native)").
		WithSize(windowWidth, windowHeight))

	uiApp := app.New(
		app.WithWindowProvider(gogpuApp),
		app.WithPlatformProvider(gogpuApp),
		app.WithEventSource(gogpuApp.EventSource()),
		app.WithTheme(m3.AsTheme()),
	)

	st := newState(pbApp, pbstore.New(pbApp, todosCollection))
	tokPath := tokenPath(cfg.DataDir)
	restoreSession(st, tokPath)

	// uiMu serializes every widget-tree rebuild (SetRoot). Event
	// callbacks run on the UI thread; the poll goroutine below does
	// not — both funnel through refreshUI, so both take uiMu. Lock
	// order is always uiMu → stateMu (snapshot inside buildRoot),
	// never the reverse. refresh() takes stateMu alone and never
	// touches widgets, so the poll's I/O never blocks the UI.
	var uiMu sync.Mutex
	deps := uiDeps{s: st}
	deps.refreshUI = func() {
		uiMu.Lock()
		defer uiMu.Unlock()
		uiApp.SetRoot(buildRoot(deps))
		gogpuApp.RequestRedraw()
	}
	// Sign-out is routed through main (not s.signOut() directly) so
	// the persisted session file is removed together with the state
	// reset — otherwise a stale token would silently re-login on the
	// next launch after an explicit sign-out.
	deps.onSignOut = makeSignOut(st, tokPath)
	uiApp.SetRoot(buildRoot(deps))

	// One poll loop drives both background duties (realtime-read +
	// session persistence) so there is exactly one background
	// goroutine to reason about. Stops when the window closes
	// (desktop.Run returns).
	done := make(chan struct{})
	defer close(done)
	go startPollLoop(st, tokPath, deps.refreshUI, pollInterval, done)

	if err := desktop.Run(gogpuApp, uiApp); err != nil {
		return fmt.Errorf("desktop run: %w", err)
	}
	return nil
}

// makeSignOut returns the full sign-out action: state reset plus
// persisted-session removal. A standalone function (not an inline
// closure) so tests can invoke the exact wiring the button uses.
func makeSignOut(st *windowState, tokPath string) func() {
	return func() {
		st.signOut()
		clearToken(tokPath)
	}
}

// startPollLoop re-lists the collection on every tick so other
// frontends' writes appear without user action, rebuilds the UI, and
// persists a fresh login token. I/O (refresh) runs off the UI thread
// — refresh is pure data under stateMu and never touches widgets;
// only refreshUI takes uiMu. Login persistence rides the same tick so
// views.go never touches the filesystem: the first tick after login
// saves the fresh token (0600), later ticks are no-ops while the
// token matches.
func startPollLoop(st *windowState, tokPath string, refreshUI func(), interval time.Duration, done <-chan struct{}) {
	ticker := time.NewTicker(interval)
	defer ticker.Stop()
	for {
		select {
		case <-done:
			return
		case <-ticker.C:
			st.refresh()
			refreshUI()
			persistSessionIfNeeded(st, tokPath)
		}
	}
}

// restoreSession adopts a saved token when it still resolves to an
// owner. Expired, revoked, or missing tokens clear the file and fall
// through to the login form — fail fast to login, never a stuck
// "signed in but empty" frame.
func restoreSession(st *windowState, tokPath string) {
	token, err := loadToken(tokPath)
	if err != nil {
		return
	}
	ownerID, err := auth.ResolveOwner(st.app, token)
	if err != nil {
		slog.Warn("gui: saved session no longer valid, clearing", "error", err)
		clearToken(tokPath)
		return
	}
	st.adoptSession(token, ownerID)
}

// persistSessionIfNeeded saves the live session token (0600) once per
// sign-in. A token that appeared without a matching file is always
// worth saving; removal happens explicitly in onSignOut.
func persistSessionIfNeeded(st *windowState, tokPath string) {
	snap := st.snapshot()
	if !snap.signedIn || snap.ownerID == "" {
		return
	}
	st.mu.Lock()
	token := st.token
	st.mu.Unlock()
	if token == "" {
		return
	}
	if existing, err := loadToken(tokPath); err == nil && existing == token {
		return
	}
	if err := saveToken(tokPath, token); err != nil {
		slog.Warn("gui: could not persist session", "error", err)
	}
}

// Compile-time proof of the reuse claim: the store handed to the native
// window is the exact type the HTTP handlers use, and the auth entry
// point is transport-free.
var (
	_ = pbstore.New
	_ = auth.Login
	_ *pocketbase.PocketBase
)
