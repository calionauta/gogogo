package main

import (
	"testing"

	"github.com/calionauta/gogogo/config"
)

// TestListenAddrHonorsChiAddr pins the G4 contract: CHI_ADDR overrides
// Host:Port when set, and the default stays Host:Port when empty. Before
// the fix, run() ignored ChiAddr outright, so the env var was dead on
// the main binary (only cmd/desktop honored it via StartChi).
func TestListenAddrHonorsChiAddr(t *testing.T) {
	cfg := config.Load()
	cfg.Host = "127.0.0.1"
	cfg.Port = 8080

	if got := listenAddr(cfg); got != "127.0.0.1:8080" {
		t.Errorf("empty ChiAddr: listenAddr = %q, want Host:Port", got)
	}

	cfg.ChiAddr = "127.0.0.1:9099"
	if got := listenAddr(cfg); got != "127.0.0.1:9099" {
		t.Errorf("set ChiAddr: listenAddr = %q, want the override", got)
	}
}
