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
	"fmt"
	"log"
	"net/http/httputil"
	"net/url"
	"time"

	"github.com/wailsapp/wails/v3/pkg/application"

	"github.com/calionauta/gogogo/config"
	"github.com/calionauta/gogogo/internal/collab"
	"github.com/calionauta/gogogo/internal/nats"
	"github.com/calionauta/gogogo/internal/server"
)

//nolint:gocyclo // extracting NATS+Collab would add abstraction over single-use setup
func main() {
	cfg := config.Load()

	// Edge sync (Phase B): if a central NATS URL is configured, boot this
	// instance as a Leaf Node so its JetStream streams replicate to/from
	// the central server (offline edits replay on reconnect). Otherwise
	// start a standalone embedded NATS for local realtime only.
	var (
		js  nats.JetStreamLike
		nat *nats.Handle
	)
	if cfg.NATS.LeafNodeURL != "" {
		h, err := nats.StartLeafNode(cfg.NATS.StoreDir, cfg.NATS.LeafNodeURL)
		if err != nil {
			log.Printf("WARN: leaf node start failed, falling back to standalone NATS: %v", err)
		} else {
			nat, js = h, h.JS // leaf-node transport (nat feeds collab only)
		}
	} else if cfg.NATS.Enabled {
		h, err := nats.StartEmbedded(cfg.NATS.StoreDir)
		if err != nil {
			log.Printf("WARN: NATS start failed, in-memory broadcaster only: %v", err)
		} else {
			nat, js = h, h.JS // embedded transport (nat feeds collab only)
		}
	}

	addr := fmt.Sprintf("%s:%d", cfg.Host, cfg.Port)
	// Point the flat stack at the address the webview proxy targets, so
	// the seam is explicit here instead of relying on an env var the
	// desktop launcher would have to set.
	cfg.ChiAddr = addr

	// Boot the shared backend. js is the JetStream the publisher uses
	// (Leaf Node or standalone); nil falls back to in-process SSE.
	// StartChi returns a nil stop with a nil error only when ChiAddr is
	// empty, which cannot happen here.
	stopChi, err := server.StartChi(cfg, js)
	if err != nil {
		log.Fatalf("boot failed: %v", err)
	}
	if stopChi == nil {
		log.Fatalf("boot failed: chi stack did not start (no %s listener)", addr)
	}

	// Edge sync (Phase C): publish local Loro updates on app.sync.<docID>.
	// When running as a Leaf Node, these replicate to the central server
	// (and the central SyncWorker persists them to Turso). When
	// standalone, they fan out to local realtime subscribers. The doc
	// edit below is a minimal end-to-end smoke of the publish path.
	if js != nil && nat != nil && nat.Conn != nil {
		pub := collab.NewPublisher(nat.Conn)
		demoDoc := collab.NewDoc("desktop-demo")
		if pubErr := pub.PublishUpdate(demoDoc, nil); pubErr != nil {
			log.Printf("WARN: collab publish failed: %v", pubErr)
		} else {
			log.Printf("desktop: published collab update for %s", demoDoc.ID())
		}

		// Ephemeral presence: broadcast this desktop's cursor on the demo
		// doc so other edges / the browser see it live. A real whiteboard
		// UI would call PublishCursor on pointer move; here we tick a demo
		// cursor so the presence path is exercised end-to-end.
		pres := collab.NewPresence(nat.Conn, "desktop-demo", "desktop", 0, 0)
		presCtx, presCancel := context.WithCancel(context.Background())
		defer presCancel()
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
					x = (x + 0.1)
					if x > 1 {
						x = 0
					}
					_ = pres.PublishCursor(x, 0.5) //nolint:mnd // 0.5 is center-Y for demo cursor
				}
			}
		}()
	}

	// Reverse proxy: the Wails webview loads http://localhost:<port>
	// (where the flat stack serves the UI) through this handler.
	target, err := url.Parse("http://" + addr)
	if err != nil {
		//nolint:gocritic // log.Fatalf is intentional — main() exits here.
		log.Fatalf("parse target url: %v", err)
	}
	proxy := httputil.NewSingleHostReverseProxy(target)

	app := application.New(application.Options{
		Name: "Gogogo Template",
		Assets: application.AssetOptions{
			Handler: proxy,
		},
		// Shut the backend down when the Wails app closes (window quit),
		// not via defer — defer would be skipped on a fatal error and the
		// gocritic linter flags it. OnShutdown runs on a clean exit.
		OnShutdown: func() {
			stopChi()
		},
	})

	log.Printf("desktop: flat stack on %s, webview proxied", addr)
	if runErr := app.Run(); runErr != nil {
		log.Fatalf("wails run: %v", runErr)
	}
}
