// Command mygo is the MyGo peer shell over the exact same flat stack the
// web app uses. It boots the stack via internal/server.StartChi, serves it
// on an ephemeral loopback port, and opens a MyGo webview window on it —
// the MyGo counterpart of cmd/desktop (Wails).
//
// Why a loopback URL and not a Wails asset handler: WebKit does not persist
// cookies set on a custom scheme, so the session cookie never came back.
// A real loopback origin keeps cookies, SameSite=Lax, CSP nonces,
// the service worker and SSE behaving exactly as they do in the browser.
// The spike proof (docs/research/mygo-2026-11.md) closed the one gap that
// blocked this: with ENVIRONMENT=dev (SecureCookies=false) and an
// isolated TURSO_DB_PATH the webview persists gogogo_auth, GET
// /api/todos 200, POST returns the SSE datastar-patch.
//
// This is a separate build target, not part of the default web build/test
// loop, and no CI job compiles it (like cmd/desktop and cmd/gui).
// make mygo is the gate; scripts/web-packages.sh excludes it.
package main

import (
	"log/slog"
	"net"
	"os"
	"strconv"

	natsio "github.com/nats-io/nats.go"

	"github.com/calionauta/gogogo/config"
	"github.com/calionauta/gogogo/internal/cloudcfg"
	"github.com/calionauta/gogogo/internal/desktopboot"
	"github.com/calionauta/gogogo/internal/nats"
	"github.com/calionauta/gogogo/internal/server"

	"github.com/egoist/mygo"
)

func main() {
	// The single-instance lock is acquired here, before the backend boots:
	// a duplicate launch must not open a second listener, a second NATS and
	// a second writer on the same database file. MyGo's lock is keyed by
	// application name (+ identifier when packaged), not by database path
	// like Wails — so two profiles (different TURSO_DB_PATH) share one lock
	// in v1; the opener exits cleanly and the first instance receives the
	// args (OnSecondInstance). A second backend would only die later on
	// Turso's "File is locked" error — the lock is the clean gate.
	if !mygo.App.RequestSingleInstanceLock() {
		slog.Info("mygo: second instance, exiting")
		return
	}
	cfg := mygoConfig()
	desktopboot.ScopeInstanceIdentity(cfg)

	nat, js := startTransport(cfg)

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
	defer stopChi()
	port, perr := desktopboot.PortOf(addr)
	if perr != nil {
		slog.Error("boot failed: unparsable listener address", "addr", addr, "error", perr)
		os.Exit(1)
	}

	viewURL := desktopboot.WebviewURL(cfg.Host, port)
	slog.Info("mygo: window on flat stack", "addr", addr, "url", viewURL)
	mygo.App.WhenReady(func() {
		mygo.NewWindow(mygo.WindowOptions{
			Title:  "Gogogo Template",
			Width:  1280,
			Height: 860,
			URL:    viewURL,
			Page:   mygo.PageOptions{TrustedOrigins: []string{viewURL}},
		})
	})
	if runErr := mygo.App.Run(); runErr != nil {
		slog.Error("mygo run", "error", runErr)
		os.Exit(1)
	}
}

// mygoConfig loads config and applies the shell's overrides. The shell has
// exactly one client — its own webview — so it listens on loopback unless
// the operator names a HOST. SecureCookies is cleared because WebKit
// drops a Secure cookie over http://127.0.0.1; loopback is the only hop
// the cookie crosses, where Secure protects nothing. The shell is the
// thing that is gated (LicenseGate=true); the web deploy stays the issuer.
func mygoConfig() *config.Config {
	cfg := config.Load()
	if os.Getenv("HOST") == "" {
		cfg.Host = "127.0.0.1"
	}
	cfg.Auth.SecureCookies = false
	cfg.LicenseGate = true
	applySyncConfig(cfg)
	return cfg
}

// applySyncConfig turns the connection saved at /account into the NATS
// leaf node the shell boots with. An explicit NATS_LEAFNODE_URL always
// wins, so an operator's environment is never overridden by a UI setting.
func applySyncConfig(cfg *config.Config) {
	if os.Getenv("NATS_LEAFNODE_URL") != "" {
		return
	}
	c, err := cloudcfg.Load(cloudcfg.Path(cfg.DataDir))
	if err != nil {
		slog.Warn("mygo: cloud sync config ignored", "error", err)
		return
	}
	if c == nil {
		return
	}
	leaf, lerr := cloudcfg.LeafURL(*c)
	if lerr != nil {
		slog.Warn("mygo: cloud sync URL invalid", "error", lerr)
		return
	}
	cfg.NATS.Enabled = true
	cfg.NATS.LeafNodeURL = leaf
	slog.Info("mygo: cloud sync configured", "url", c.URL)
}

// startTransport boots the realtime transport. A configured central NATS
// URL makes this instance a Leaf Node; otherwise a standalone embedded
// NATS serves local realtime. A failed leaf start falls back to the
// embedded server; only a failed embedded start returns nil (SSE only).
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
