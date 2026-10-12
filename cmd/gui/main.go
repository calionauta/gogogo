// SCOPE:feature — gogpu/ui native window over the existing backend
//
// Command gui is a proof of concept: a native desktop window built with
// gogpu/ui (pure-Go GPU rendering, zero CGO, no webview, no JavaScript)
// that runs on the SAME backend as the web app — the flat stack: owned
// auth (internal/auth), the sqlc todos (internal/db), and the todo model.
//
// What this proves, and what it does not:
//
//	Proved: the domain layer is genuinely transport-agnostic. Auth
//	  (internal/auth.Login + SessionByToken), persistence (the same
//	  dbgen queries the chi handlers call) and the todo model are
//	  consumed here with no *http.Request, no cookie, and no HTTP
//	  router. If the web app could be deleted and this app kept, the
//	  layering held. Cross-frontend visibility holds too: a todo created
//	  in the web app appears here within one poll tick (same database).
//	Not proved: no offline sync (this window is ONLINE-ONLY — mutations
//	  run synchronously against the local DB and fail visibly when it
//	  is unreachable; there is no outbox, no replay, no Loro merge
//	  see the whiteboard's whiteboard.js outbox for what that would
//	  take), no durable workflow trigger (handleCreate resumes the
//	  onboarding workflow; the native add() does not — the trigger
//	  lives in the HTTP layer and needs extracting to the store
//	  before both frontends inherit it), no multi-user push (polling
//	  only, no JetStream subscription), no theming parity with the
//	  web skins.
//
// Note there is no HTTP listener here. db.Open + db.Migrate prepare the
// database without serving; the store reads and writes through the
// queries directly. That is the proof in miniature — the web tier is
// genuinely optional for a native frontend.
//
// It is deliberately NOT part of `make build` / `make test`: it is a
// separate native target like cmd/desktop, validated by the gui job
// in .github/workflows/desktop.yml (CGO_ENABLED=0, pure Go, so it builds
// headless anywhere `go` is; tests run headless too, no window/GPU).
//
// To remove: delete cmd/gui/, drop the `gui` target from the Makefile,
// drop the gui job from desktop.yml, and re-add cmd/gui to
// scripts/web-packages.sh's exclusion... (it is currently excluded;
// removing the directory needs no script change).
package main

import (
	"context"
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

	"github.com/calionauta/gogogo/config"
	"github.com/calionauta/gogogo/internal/auth"
	"github.com/calionauta/gogogo/internal/db"
	dbgen "github.com/calionauta/gogogo/internal/db/sqlcgen"
	"github.com/calionauta/gogogo/internal/demo"

	_ "github.com/ncruces/go-sqlite3/driver"
)

// pollInterval is the realtime-read tick: how often the window
// re-reads the collection. No push channel exists in the native window, so this
// is what makes another frontend's writes visible here. 2s is the
// KISS point — fast enough to feel live, slow enough that a local
// SQLite re-list never shows up in profiles.
const pollInterval = 2 * time.Second

// Window chrome for the native window.
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

	// The same flat stack the web app serves: Turso file + migrations +
	// owned auth, minus the HTTP listener. No framework, no bootstrap
	// hook — the queries below are the ones the chi handlers call.
	sqlDB, err := db.Open(cfg)
	if err != nil {
		return fmt.Errorf("turso open: %w", err)
	}
	defer sqlDB.Close()
	if merr := db.Migrate(sqlDB); merr != nil {
		return fmt.Errorf("migrate: %w", merr)
	}

	a, aerr := auth.New(dbgen.New(sqlDB), !cfg.Dev)
	if aerr != nil {
		return fmt.Errorf("auth: %w", aerr)
	}
	if len(cfg.Auth.TOTPEncKey) > 0 {
		if err := a.SetTOTPEncKey(cfg.Auth.TOTPEncKey); err != nil {
			return fmt.Errorf("auth: %w", err)
		}
	}
	demo.Ensure(context.Background(), a)

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

	st := newState(guiStore{q: dbgen.New(sqlDB)}, a)
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

// startPollLoop re-lists the store on every tick so other
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
	sess, err := st.a.SessionByToken(context.Background(), token)
	if err != nil {
		slog.Warn("gui: saved session no longer valid, clearing", "error", err)
		clearToken(tokPath)
		return
	}
	st.adoptSession(token, sess.User.ID)
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
