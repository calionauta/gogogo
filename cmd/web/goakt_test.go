// GoAkt lifecycle red-team tests.
package main

import (
	"context"
	"testing"

	"github.com/calionauta/gogogo/config"
	appgoakt "github.com/calionauta/gogogo/internal/goakt"
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

// TestStartGoAktAdoptsTheRecipeHost is the double-boot regression: the room
// recipe starts a host during BootChi and publishes it as the process default.
// startGoAkt must ADOPT it, not start a second system — two systems with the
// same name left the presence upgrade on one and the shutdown on the other.
func TestStartGoAktAdoptsTheRecipeHost(t *testing.T) {
	cfg := &config.Config{}
	cfg.GoAkt.Enabled = true

	// Stand in for the recipe: a started host published as the default.
	recipeHost := appgoakt.New()
	if err := recipeHost.Start(context.Background()); err != nil {
		t.Fatalf("recipe host start: %v", err)
	}
	appgoakt.SetDefault(recipeHost)
	goAktHost = nil
	t.Cleanup(func() {
		appgoakt.SetDefault(nil)
		_ = recipeHost.Stop(context.Background())
		goAktHost = nil
	})

	startGoAkt(context.Background(), cfg)
	if goAktHost != recipeHost {
		t.Error("startGoAkt started a second actor system instead of adopting the recipe's")
	}
	if appgoakt.Default() != recipeHost {
		t.Error("the process default no longer names the host the routes use")
	}
}
