// SCOPE:layer=infra,removal=core — Tests for the transport-free auth entry points.
package auth_test

import (
	"bytes"
	"context"
	"crypto/rand"
	"encoding/hex"
	"errors"
	"os"
	"strings"
	"testing"

	"github.com/pocketbase/pocketbase"
	"github.com/pocketbase/pocketbase/core"

	"github.com/calionauta/gogogo/config"
	"github.com/calionauta/gogogo/db"
	"github.com/calionauta/gogogo/features/auth"

	_ "github.com/ncruces/go-sqlite3/driver"
)

// These tests cover the seam a non-HTTP frontend depends on: Login
// mints a token without any cookie/RequestEvent, and ResolveOwner maps
// that token back to the ownerID the EntityStore contract expects.
// HandlePasswordLogin is a thin wrapper over the same Login call, so
// proving these two is what proves both frontends agree on who the
// user is.

const (
	testEmail    = "native@demo.app"
	testPassword = "testpassword"
)

func newSeededApp(t *testing.T) *pocketbase.PocketBase {
	t.Helper()

	tmpDir := t.TempDir()
	key := make([]byte, 32)
	if _, err := rand.Read(key); err != nil {
		t.Fatalf("rand key: %v", err)
	}
	cfg := &config.Config{
		Host:          "127.0.0.1",
		Dev:           true,
		DataDir:       tmpDir,
		DBPath:        tmpDir + "/app.db",
		EncryptionKey: hex.EncodeToString(key),
	}

	app, err := db.Init(cfg)
	if err != nil {
		t.Fatalf("db.Init: %v", err)
	}
	if err = app.Bootstrap(); err != nil {
		t.Fatalf("Bootstrap: %v", err)
	}
	if err = db.SeedDefaults(app, true); err != nil {
		t.Fatalf("SeedDefaults: %v", err)
	}

	col, err := app.FindCollectionByNameOrId("users")
	if err != nil {
		t.Fatalf("find users collection: %v", err)
	}
	rec := core.NewRecord(col)
	rec.SetEmail(testEmail)
	rec.SetPassword(testPassword)
	rec.Set("verified", true)
	if err := app.Save(rec); err != nil {
		t.Fatalf("save test user: %v", err)
	}
	return app
}

// A successful Login returns a token that ResolveOwner maps back to the
// same record id — the exact value RequireOwner yields for a request
// carrying that token's cookie.
func TestLoginThenResolveOwnerRoundTrip(t *testing.T) {
	app := newSeededApp(t)

	userID, token, err := auth.Login(app, testEmail, testPassword)
	if err != nil {
		t.Fatalf("Login: %v", err)
	}
	if token == "" {
		t.Error("Login returned an empty token")
	}

	ownerID, err := auth.ResolveOwner(app, token)
	if err != nil {
		t.Fatalf("ResolveOwner: %v", err)
	}
	if ownerID != userID {
		t.Errorf("owner mismatch: Login gave %q, ResolveOwner gave %q", userID, ownerID)
	}
}

// A bad email and a wrong password must be indistinguishable, so the
// error cannot be used to enumerate which accounts exist.
func TestLoginRejectsBadCredentialsIndistinguishably(t *testing.T) {
	app := newSeededApp(t)

	cases := map[string][2]string{
		"unknown email":  {"nobody@demo.app", testPassword},
		"wrong password": {testEmail, "not-the-password"},
		"empty email":    {"", testPassword},
		"empty password": {testEmail, ""},
	}
	for name, tc := range cases {
		t.Run(name, func(t *testing.T) {
			userID, token, err := auth.Login(app, tc[0], tc[1])
			if err == nil {
				t.Fatalf("Login succeeded for %q (user=%q)", tc[0], userID)
			}
			if !errors.Is(err, auth.ErrBadCredentials) {
				t.Errorf("got %v, want ErrBadCredentials", err)
			}
			if token != "" {
				t.Error("a rejected login must not return a token")
			}
		})
	}
}

// ResolveOwner is the trust boundary for a native window: an empty or
// garbage token must not resolve to an owner, or every store call would
// run unscoped.
func TestResolveOwnerRejectsBadToken(t *testing.T) {
	app := newSeededApp(t)

	for _, token := range []string{"", "not-a-real-token"} {
		if ownerID, err := auth.ResolveOwner(app, token); err == nil {
			t.Errorf("ResolveOwner(%q) succeeded with owner %q, want error", token, ownerID)
		}
	}
}

// TestThemeToggleSingleOwner pins the wiring that broke the toggle live:
// exactly one toggle path (the delegated listener on .theme-toggle,
// which works with and without the Datastar runtime) and one storage key
// shared with ThemeHead. An inline data-on:click would double-fire where
// Datastar exists and lie dead where it doesn't (whiteboard board, notes);
// a second storage key orphans the persisted choice on OS-preference
// changes.
func TestThemeToggleSingleOwner(t *testing.T) {
	var buf bytes.Buffer
	if err := auth.Navbar("demo1@demo.app", "todo", "dev", "c0ffee").Render(context.Background(), &buf); err != nil {
		t.Fatalf("render navbar: %v", err)
	}
	html := buf.String()
	start := strings.Index(html, "app-theme-toggle")
	if start < 0 {
		t.Fatalf("navbar missing the theme toggle button")
	}
	end := strings.Index(html[start:], "</button>")
	if end < 0 {
		t.Fatalf("navbar theme button never closes")
	}
	button := html[start : start+end]
	if !strings.Contains(button, "theme-toggle") {
		t.Errorf("theme button missing the .theme-toggle class the delegated listener binds")
	}
	if strings.Contains(button, "data-on:click") {
		t.Errorf("theme button must not carry data-on:click " +
			"(double-fire with the delegated listener; dead on Datastar-less pages)")
	}

	// One storage key across the pre-paint bootstrap and the module.
	head, err := os.ReadFile("theme_head.templ")
	if err != nil {
		t.Fatalf("read theme_head: %v", err)
	}
	// web/resources/static/theme.js is read from the repo root's static tree.
	js, err := os.ReadFile("../../web/resources/static/theme.js")
	if err != nil {
		t.Fatalf("read theme.js: %v", err)
	}
	for _, src := range []string{string(head), string(js)} {
		if !strings.Contains(src, "themeMode") {
			t.Errorf("theme wiring left the shared storage key")
		}
	}
	if strings.Contains(string(js), `var KEY = "theme";`) {
		t.Errorf("theme.js uses an orphan storage key (ThemeHead reads themeMode)")
	}
}
