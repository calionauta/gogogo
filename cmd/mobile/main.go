//go:build android

// Command mobile is the Android spike entrypoint: it boots the same flat
// server the desktop shell uses on a fixed loopback port, and the Android
// activity loads that origin directly. The desktop entrypoint cannot be reused
// because it opens a desktop window and its Wails asset origin is Java-side.
package main

import (
	"log/slog"
	"os"
	"path/filepath"

	"github.com/wailsapp/wails/v3/pkg/application"

	"github.com/calionauta/gogogo/config"
	"github.com/calionauta/gogogo/internal/server"
)

func main() {
	cfg := mobileConfig()
	stop, addr, err := server.StartChi(cfg, nil, nil)
	if err != nil {
		slog.Error("mobile: boot failed", "error", err)
		os.Exit(1)
	}
	if stop != nil {
		defer stop()
	}
	app := application.New(application.Options{
		Name:        "gogogo",
		Description: "gogogo mobile spike",
	})
	app.Window.NewWithOptions(application.WebviewWindowOptions{
		Title: "gogogo",
		URL:   "/",
	})
	slog.Info("mobile: serving", "addr", addr, "data", cfg.DataDir)
	if runErr := app.Run(); runErr != nil {
		slog.Error("mobile: wails run", "error", runErr)
		os.Exit(1)
	}
}

// mobileConfig is the Android profile: loopback, open auth (no login/cookies
// in a webview asset origin), no NATS/DagNats/GoAkt, and the app-private
// files directory Wails exposes for the database.
func mobileConfig() *config.Config {
	cfg := config.Load()
	cfg.Host = "127.0.0.1"
	cfg.Port = 0
	// Fixed loopback port: the Android activity loads this URL directly (the
	// Wails asset origin is a Java-side loader and cannot proxy to Go).
	cfg.ChiAddr = "127.0.0.1:18080"
	cfg.AuthMode = config.AuthOpen
	cfg.LicenseMode = config.LicenseNone
	cfg.LicenseGate = false
	cfg.Auth.SecureCookies = false
	cfg.SignupEnabled = false
	cfg.DemoAccounts = false
	cfg.NATS.Enabled = false
	cfg.DagNats.Enabled = false
	cfg.GoAkt.Enabled = false
	cfg.OfflineSync.Enabled = false
	cfg.HTTPAccessLog = true
	cfg.BuildLabel = "mobile"
	dataDir := application.Mobile.StoragePath()
	if dataDir == "" {
		dataDir = os.TempDir()
	}
	cfg.DataDir = dataDir
	cfg.Turso.DBPath = filepath.Join(dataDir, "app.db")
	return cfg
}
