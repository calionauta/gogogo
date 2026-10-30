// Command desktop builds the Wails v3 native app shell around the exact
// same Go backend the web app uses (goqite queue + handlers + the flat
// chi router). It boots the stack via internal/server.StartChi, serves it
// in the background, and points the Wails webview at it through a reverse
// proxy — so the desktop app and the web app share 100% of the business
// logic.
//
// When NATS_LEAFNODE_URL is set, the desktop becomes a NATS Leaf Node
// that syncs its JetStream streams with the central server (NATS_LEAFNODE_URL)
// — Phase B of the edge-sync design. Phase C (Loro CRDT collab) builds on
// top of this transport.
package main

import (
	"context"
	"crypto/sha256"
	"fmt"
	"log/slog"
	"net"
	"os"
	"strconv"
	"time"

	natsio "github.com/nats-io/nats.go"
	"github.com/wailsapp/wails/v3/pkg/application"

	"github.com/calionauta/gogogo/config"
	"github.com/calionauta/gogogo/internal/cloudcfg"
	"github.com/calionauta/gogogo/internal/collab"
	"github.com/calionauta/gogogo/internal/instance"
	"github.com/calionauta/gogogo/internal/nats"
	"github.com/calionauta/gogogo/internal/server"
)

func main() {
	cfg := desktopConfig()
	// The CRUD wire tags every op with this process's instance id and drops
	// ops that carry its own id. The server and the desktop often run on the
	// same host (dev, a self-hosted box), where the hostname-derived id is
	// identical and the server would discard the desktop's writes as its own
	// echo. Scope the desktop id to its profile (the database path), the same
	// key the single-instance lock uses.
	scopeInstanceIdentity(cfg)

	// The single-instance lock is acquired here, before the backend boots: a
	// duplicate launch must not open a second listener, a second NATS and a
	// second writer on the same database file. Wails exits the loser.
	var stopBackend func()
	app := application.New(application.Options{
		Name: "Gogogo Template",
		// One instance per profile: scoped by the database path so two
		// profiles (different TURSO_DB_PATH) can still run side by side.
		SingleInstance: &application.SingleInstanceOptions{
			UniqueID: singleInstanceID(cfg.AppName, cfg.Turso.DBPath),
			// The running window is already the app; a duplicate launch is
			// a no-op, not an error.
			OnSecondInstanceLaunch: func(application.SecondInstanceData) {},
		},
		// Closing the window quits the app. Without it macOS keeps the
		// process alive with no window and no way to quit it.
		Mac: application.MacOptions{
			ApplicationShouldTerminateAfterLastWindowClosed: true,
		},
		// Shut the backend down when the Wails app closes (window quit),
		// not via defer — defer would be skipped on a fatal error and the
		// gocritic linter flags it. OnShutdown runs on a clean exit.
		OnShutdown: func() {
			if stopBackend != nil {
				stopBackend()
			}
		},
	})

	nat, js := startTransport(cfg)

	// Bind an ephemeral port unless the operator pinned PORT: a fixed default
	// (8080) is taken by any dev server, and the shell must not refuse to open
	// because of it. The kernel-chosen address comes back from StartChi and is
	// what the window loads.
	portArg := "0"
	if os.Getenv("PORT") != "" {
		portArg = strconv.Itoa(cfg.Port)
	}
	cfg.ChiAddr = net.JoinHostPort(cfg.Host, portArg)

	var nc *natsio.Conn
	if nat != nil {
		nc = nat.Conn
	}
	stopChi, addr, err := server.StartChi(cfg, nc, js)
	if err != nil {
		slog.Error("boot failed", "error", err)
		os.Exit(1)
	}
	if stopChi == nil {
		slog.Error("boot failed: chi stack did not start", "addr", cfg.ChiAddr)
		os.Exit(1)
	}
	stopBackend = stopChi
	port, perr := portOf(addr)
	if perr != nil {
		slog.Error("boot failed: unparsable listener address", "addr", addr, "error", perr)
		stopChi()
		os.Exit(1)
	}

	// Edge sync (Phase C): a demo publish keeps the edge path exercised.
	defer publishDemoCollab(nat, js)()

	// The window loads the flat stack over loopback HTTP, not through a
	// Wails asset handler: WebKit does not persist cookies set on a custom
	// scheme (wails://), so the session cookie never came back and every
	// page rendered logged out. A real loopback origin keeps cookies,
	// SameSite=Lax, CSP nonces, the service worker and SSE behaving
	// exactly as they do in the browser.
	viewURL := webviewURL(cfg.Host, port)
	app.Window.NewWithOptions(application.WebviewWindowOptions{
		Name:   "main",
		Title:  "Gogogo Template",
		Width:  1280,
		Height: 860,
		URL:    viewURL,
	})

	slog.Info("desktop: window on flat stack", "addr", addr, "url", viewURL)
	if runErr := app.Run(); runErr != nil {
		slog.Error("wails run", "error", runErr)
		os.Exit(1)
	}
}

// desktopConfig loads config and applies the shell's overrides. Split out of
// main so each decision carries its reason in one place.
func desktopConfig() *config.Config {
	cfg := config.Load()

	// The shell has exactly one client — its own webview — so it listens on
	// loopback unless the operator names a HOST. That also keeps the
	// non-Secure session cookie below off any network.
	if os.Getenv("HOST") == "" {
		cfg.Host = "127.0.0.1"
	}
	// WebKit drops a Secure cookie on a non-HTTPS origin (verified against
	// 127.0.0.1 with a WKWebView harness), so a production config would
	// leave every page logged out. Loopback is the only hop the cookie
	// crosses, where Secure protects nothing.
	cfg.Auth.SecureCookies = false
	// The shell is the thing that is gated. A web deploy in account mode is
	// the issuer and must stay reachable for login and device tokens.
	cfg.LicenseGate = true
	applySyncConfig(cfg)
	return cfg
}

// applySyncConfig turns the connection saved at /account into the NATS leaf
// node the shell boots with. An explicit NATS_LEAFNODE_URL always wins, so an
// operator's environment is never overridden by a UI setting.
func applySyncConfig(cfg *config.Config) {
	if os.Getenv("NATS_LEAFNODE_URL") != "" {
		return
	}
	c, err := cloudcfg.Load(cloudcfg.Path(cfg.DataDir))
	if err != nil {
		slog.Warn("desktop: cloud sync config ignored", "error", err)
		return
	}
	if c == nil {
		return
	}
	leaf, lerr := cloudcfg.LeafURL(*c)
	if lerr != nil {
		slog.Warn("desktop: cloud sync URL invalid", "error", lerr)
		return
	}
	cfg.NATS.Enabled = true
	cfg.NATS.LeafNodeURL = leaf
	slog.Info("desktop: cloud sync configured", "url", c.URL)
}

// scopeInstanceIdentity pins APP_INSTANCE_ID to the desktop profile unless the
// operator already set one. instance.ID() caches on first use, so this must run
// before the backend boots.
func scopeInstanceIdentity(cfg *config.Config) {
	if os.Getenv(instance.EnvVar) != "" {
		return
	}
	sum := sha256.Sum256([]byte(cfg.Turso.DBPath))
	os.Setenv(instance.EnvVar, fmt.Sprintf("%s-desktop-%x", cfg.AppName, sum[:8]))
}

// singleInstanceID scopes the Wails lock to the app AND its database, so a
// second profile still opens while a duplicate of the same profile is refused.
func singleInstanceID(appName, dbPath string) string {
	sum := sha256.Sum256([]byte(dbPath))
	return fmt.Sprintf("%s.desktop.%x", appName, sum[:8])
}

// portOf parses the port out of a bound listener address.
func portOf(addr string) (int, error) {
	_, portStr, err := net.SplitHostPort(addr)
	if err != nil {
		return 0, err
	}
	port, err := strconv.Atoi(portStr)
	if err != nil {
		return 0, fmt.Errorf("port %q: %w", portStr, err)
	}
	return port, nil
}

// webviewURL is the loopback URL the window loads. cfg.Host is the LISTEN
// address (the wildcard unless HOST names one); a webview cannot navigate to
// 0.0.0.0, so the wildcard maps to 127.0.0.1 — the same process is serving
// there.
func webviewURL(host string, port int) string {
	switch host {
	case "", "0.0.0.0", "::", "[::]":
		host = "127.0.0.1"
	}
	return "http://" + net.JoinHostPort(host, strconv.Itoa(port)) + "/"
}

// startTransport boots the realtime transport. Edge sync (Phase B): a
// configured central NATS URL makes this instance a Leaf Node, so its
// JetStream streams replicate to/from the central server (offline edits replay
// on reconnect). Otherwise a standalone embedded NATS serves local realtime.
// A failed leaf start falls back to the embedded server: losing cross-instance
// sync must not cost the local realtime layer too. Only a failed embedded
// start returns nil (in-process SSE).
func startTransport(cfg *config.Config) (*nats.Handle, nats.JetStreamLike) {
	if cfg.NATS.LeafNodeURL != "" {
		h, err := nats.StartLeafNode(cfg.NATS.StoreDir, cfg.NATS.LeafNodeURL)
		if err == nil {
			return h, h.JS
		}
		slog.Warn("leaf node start failed, falling back to embedded NATS", "error", err)
	}
	if !cfg.NATS.Enabled {
		return nil, nil
	}
	h, err := nats.StartEmbeddedOn(cfg.NATS.StoreDir, cfg.NATS.Listen, cfg.NATS.Token)
	if err != nil {
		slog.Warn("NATS start failed, in-memory broadcaster only", "error", err)
		return nil, nil
	}
	return h, h.JS
}

// publishDemoCollab exercises the edge-sync publish paths end to end: a Loro
// update on app.sync.<docID> (a Leaf Node replicates it to the central server,
// whose SyncWorker persists the snapshot to Turso) and a ticking demo cursor on
// app.presence.<docID>. With a standalone NATS both fan out to local
// subscribers. Returns the cancel for the ticker.
func publishDemoCollab(nat *nats.Handle, js nats.JetStreamLike) context.CancelFunc {
	noop := func() {}
	if js == nil || nat == nil || nat.Conn == nil {
		return noop
	}
	pub := collab.NewPublisher(nat.Conn)
	demoDoc := collab.NewDoc("desktop-demo")
	if pubErr := pub.PublishUpdate(demoDoc, nil); pubErr != nil {
		slog.Warn("collab publish failed", "error", pubErr)
	} else {
		slog.Info("desktop: published collab update", "doc", demoDoc.ID())
	}

	// Ephemeral presence: a real whiteboard would call PublishCursor on
	// pointer move; the demo ticks a cursor so the path runs end to end.
	pres := collab.NewPresence(nat.Conn, "desktop-demo", "desktop", 0, 0)
	presCtx, presCancel := context.WithCancel(context.Background())
	go func() {
		_ = pres.Subscribe(presCtx)
	}()
	go func() {
		t := time.NewTicker(2 * time.Second)
		defer t.Stop()
		var x float64
		for {
			select {
			case <-presCtx.Done():
				return
			case <-t.C:
				x += 0.1
				if x > 1 {
					x = 0
				}
				_ = pres.PublishCursor(x, 0.5) //nolint:mnd // 0.5 is center-Y for demo cursor
			}
		}
	}()
	return presCancel
}
