// SCOPE:layer=infra,removal=plugin — GoAkt lifecycle red-team tests.
package main

import (
	"context"
	"testing"

	"github.com/calionauta/gogogo/config"
)

// TestShutdownGoAktWithoutStart pins nil-safety: shutdown on a boot
// that never ran (disabled engine, failed start) must not panic.
func TestShutdownGoAktWithoutStart(t *testing.T) {
	goAktHost = nil
	shutdownGoAkt()
	if goAktHost != nil {
		t.Error("shutdown must leave a nil host")
	}
}

// TestStartGoAktDisabledIsNoOp pins the off switch: disabled engine
// boots nothing and publishes nothing for the router to resolve.
func TestStartGoAktDisabledIsNoOp(t *testing.T) {
	goAktHost = nil
	startGoAkt(context.Background(), &config.Config{})
	shutdownGoAkt()
	if goAktHost != nil {
		t.Error("disabled start must not publish a host")
	}
}

// TestStartGoAktIsIdempotent pins the double-boot guard: two starts
// must not leak a second actor system behind the first.
func TestStartGoAktIsIdempotent(t *testing.T) {
	cfg := &config.Config{}
	cfg.GoAkt.Enabled = true
	goAktHost = nil
	startGoAkt(context.Background(), cfg)
	first := goAktHost
	if first == nil {
		t.Fatal("enabled start must publish a host")
	}
	startGoAkt(context.Background(), cfg)
	if goAktHost != first {
		t.Error("second start must reuse the live host, not spawn another system")
	}
	shutdownGoAkt()
	if goAktHost != nil {
		t.Error("shutdown must clear the host")
	}
}
