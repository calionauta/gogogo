package cloudcfg

import (
	"os"
	"path/filepath"
	"strings"
	"testing"
)

func TestNormalize(t *testing.T) {
	good := []struct{ in, want string }{
		{"nats://example.test:4222", "nats://example.test:4222"},
		{"  TLS://sync.example.test  ", "tls://sync.example.test"},
		{"wss://sync.example.test/nats/", "wss://sync.example.test/nats"},
	}
	for _, tc := range good {
		got, err := Normalize(tc.in)
		if err != nil || got != tc.want {
			t.Errorf("Normalize(%q) = (%q, %v), want (%q, nil)", tc.in, got, err, tc.want)
		}
	}

	bad := []string{
		"",
		"http://example.test",
		"nats://",
		"nats://user:pass@example.test:4222", // token belongs in its own field
		"nats://example.test:4222/?x=1",      // query could carry a secret past the log
	}
	for _, in := range bad {
		if _, err := Normalize(in); err == nil {
			t.Errorf("Normalize(%q) accepted", in)
		}
	}
}

func TestSaveLoadAndLeafURL(t *testing.T) {
	path := filepath.Join(t.TempDir(), "sync.json")
	if err := Save(path, Config{URL: "nats://sync.example.test:4222", Token: "device-token"}); err != nil {
		t.Fatalf("save: %v", err)
	}
	info, serr := os.Stat(path)
	if serr != nil {
		t.Fatalf("stat: %v", serr)
	}
	if perm := info.Mode().Perm(); perm != 0o600 {
		t.Fatalf("sync.json mode = %o, want 600", perm)
	}

	c, err := Load(path)
	if err != nil || c == nil {
		t.Fatalf("load: %+v, %v", c, err)
	}
	leaf, lerr := LeafURL(*c)
	if lerr != nil {
		t.Fatalf("leaf url: %v", lerr)
	}
	if !strings.HasPrefix(leaf, "nats://device-token@sync.example.test:4222") {
		t.Fatalf("leaf url = %q, token not embedded as userinfo", leaf)
	}

	if cerr := Clear(path); cerr != nil {
		t.Fatalf("clear: %v", cerr)
	}
	c, err = Load(path)
	if err != nil || c != nil {
		t.Fatalf("after clear: %+v, %v (want nil, nil)", c, err)
	}
}

func TestLoadRejectsBadFile(t *testing.T) {
	path := filepath.Join(t.TempDir(), "sync.json")
	if err := os.WriteFile(path, []byte(`{"url":"ftp://x"}`), 0o600); err != nil {
		t.Fatalf("write: %v", err)
	}
	if _, err := Load(path); err == nil {
		t.Fatal("Load accepted an unsupported scheme")
	}
}

func TestHostAllowed(t *testing.T) {
	if err := HostAllowed("nats://sync.example.test:4222", nil); err != nil {
		t.Fatalf("empty allowlist rejected: %v", err)
	}
	allows := [][]string{
		{"sync.example.test"},
		{"sync.example.test:4222"},
		{"other.test", "sync.example.test"},
	}
	for _, allow := range allows {
		if err := HostAllowed("nats://sync.example.test:4222", allow); err != nil {
			t.Fatalf("allow %v rejected: %v", allow, err)
		}
	}
	if err := HostAllowed("nats://evil.test:4222", []string{"sync.example.test"}); err == nil {
		t.Fatal("allowlist accepted another host")
	}
	if err := HostAllowed("nats://sync.example.test:4222", []string{"sync.example.test:9999"}); err == nil {
		t.Fatal("allowlist ignored the port")
	}
}
