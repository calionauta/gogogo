package main

import (
	"os"
	"strings"
	"testing"

	"github.com/calionauta/gogogo/config"
	"github.com/calionauta/gogogo/internal/cloudcfg"
	"github.com/calionauta/gogogo/internal/instance"
)

// TestWebviewURL pins the wildcard mapping: cfg.Host is the LISTEN address
// (default 0.0.0.0) and a webview cannot navigate to it, so the window URL has
// to resolve to the loopback the same process is serving on.
func TestWebviewURL(t *testing.T) {
	tests := []struct {
		name string
		host string
		port int
		want string
	}{
		{name: "wildcard v4", host: "0.0.0.0", port: 8080, want: "http://127.0.0.1:8080/"},
		{name: "wildcard v6", host: "::", port: 8080, want: "http://127.0.0.1:8080/"},
		{name: "wildcard v6 bracketed", host: "[::]", port: 8080, want: "http://127.0.0.1:8080/"},
		{name: "empty defaults to wildcard", host: "", port: 3000, want: "http://127.0.0.1:3000/"},
		{name: "explicit loopback", host: "127.0.0.1", port: 18431, want: "http://127.0.0.1:18431/"},
		{name: "ipv6 loopback", host: "::1", port: 8080, want: "http://[::1]:8080/"},
		{name: "named host", host: "localhost", port: 9000, want: "http://localhost:9000/"},
	}

	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			if got := webviewURL(tc.host, tc.port); got != tc.want {
				t.Fatalf("webviewURL(%q, %d) = %q, want %q", tc.host, tc.port, got, tc.want)
			}
		})
	}
}

// TestPortOf pins the read-back of the kernel-chosen port: the window loads the
// address the listener actually holds, never the ":0" it asked for.
func TestPortOf(t *testing.T) {
	tests := []struct {
		in      string
		want    int
		wantErr bool
	}{
		{in: "127.0.0.1:61612", want: 61612},
		{in: "[::1]:443", want: 443},
		{in: "127.0.0.1:", wantErr: true},
		{in: "nonsense", wantErr: true},
		{in: "host:port", wantErr: true},
	}
	for _, tc := range tests {
		got, err := portOf(tc.in)
		if tc.wantErr {
			if err == nil {
				t.Errorf("portOf(%q) = %d, want an error", tc.in, got)
			}
			continue
		}
		if err != nil || got != tc.want {
			t.Errorf("portOf(%q) = (%d, %v), want (%d, nil)", tc.in, got, err, tc.want)
		}
	}
}

// TestSingleInstanceID keeps two profiles apart while making duplicates of the
// same profile collapse onto one lock.
func TestSingleInstanceID(t *testing.T) {
	base := singleInstanceID("gogogo", "/data/a.db")
	if base != singleInstanceID("gogogo", "/data/a.db") {
		t.Fatal("singleInstanceID is not deterministic")
	}
	if base == singleInstanceID("gogogo", "/data/b.db") {
		t.Fatal("singleInstanceID ignores the database path; two profiles would fight over one lock")
	}
	if !strings.HasPrefix(base, "gogogo.desktop.") {
		t.Fatalf("singleInstanceID = %q, want the app-scoped prefix", base)
	}
}

// TestStartTransportDisabled pins the NATS-off contract: nil means in-process
// SSE and the stack must not be handed a half-built transport.
func TestStartTransportDisabled(t *testing.T) {
	cfg := &config.Config{}
	cfg.NATS.Enabled = false
	h, js := startTransport(cfg)
	if h != nil || js != nil {
		t.Fatalf("startTransport with NATS off = (%v, %v), want (nil, nil)", h, js)
	}
}

// TestStartTransportFallback proves a broken leaf-node URL still gets the local
// realtime layer: losing cross-instance sync must not cost embedded NATS.
func TestStartTransportFallback(t *testing.T) {
	cfg := &config.Config{}
	cfg.NATS.Enabled = true
	cfg.NATS.StoreDir = t.TempDir()
	cfg.NATS.LeafNodeURL = ":// not a url"

	h, js := startTransport(cfg)
	if h == nil || js == nil {
		t.Fatal("startTransport with a bad leaf URL = (nil, nil), want the embedded fallback")
	}
	defer h.Close()
}

// TestApplySyncConfig pins the desktop wiring of the connection saved at
// /account: a sync.json becomes the leaf node, and an explicit env var wins.
func TestApplySyncConfig(t *testing.T) {
	dir := t.TempDir()
	cfg := &config.Config{}
	cfg.DataDir = dir
	applySyncConfig(cfg)
	if cfg.NATS.LeafNodeURL != "" {
		t.Fatal("applySyncConfig invented a connection")
	}

	if err := cloudcfg.Save(cloudcfg.Path(dir), cloudcfg.Config{
		URL: "nats://sync.example.test:4222", Token: "dev-token",
	}); err != nil {
		t.Fatalf("save: %v", err)
	}
	applySyncConfig(cfg)
	if cfg.NATS.LeafNodeURL == "" || !cfg.NATS.Enabled {
		t.Fatalf("applySyncConfig did not enable the leaf node: %+v", cfg.NATS)
	}
	if !strings.Contains(cfg.NATS.LeafNodeURL, "dev-token@sync.example.test") {
		t.Fatalf("leaf url lost the token: %q", cfg.NATS.LeafNodeURL)
	}

	t.Setenv("NATS_LEAFNODE_URL", "nats://operator.example.test:4222")
	envCfg := &config.Config{}
	envCfg.DataDir = dir
	applySyncConfig(envCfg)
	if envCfg.NATS.LeafNodeURL != "" {
		t.Fatalf("env override ignored: %q", envCfg.NATS.LeafNodeURL)
	}
}

// TestScopeInstanceIdentity pins the same-host collision fix: the server and
// a desktop on one machine must not share an instance id, or the server
// discards the desktop's CRUD ops as its own echo. The id is scoped to the
// profile's database path and survives a restart (same path, same id).
func TestScopeInstanceIdentity(t *testing.T) {
	t.Setenv(instance.EnvVar, "")
	cfg := &config.Config{AppName: "gogogo"}
	cfg.Turso.DBPath = "/tmp/profile-a/app.db"
	scopeInstanceIdentity(cfg)
	a := os.Getenv(instance.EnvVar)
	if !strings.HasPrefix(a, "gogogo-desktop-") {
		t.Fatalf("instance id = %q, want a gogogo-desktop- prefix", a)
	}
	t.Setenv(instance.EnvVar, "")
	cfg.Turso.DBPath = "/tmp/profile-b/app.db"
	scopeInstanceIdentity(cfg)
	if b := os.Getenv(instance.EnvVar); b == a {
		t.Fatalf("two profiles share instance id %q", a)
	}

	// An operator's explicit id always wins.
	t.Setenv(instance.EnvVar, "pinned-by-operator")
	scopeInstanceIdentity(cfg)
	if got := os.Getenv(instance.EnvVar); got != "pinned-by-operator" {
		t.Fatalf("operator id overwritten: %q", got)
	}
}
