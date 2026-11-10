// The demo stack fixture: the real composition, two demo accounts.
//
// The tests that need a running app (the access-denied page, the dashboard
// counts) drive the SAME stack production serves — the real registry, the
// real declarations, the real pages — because every bug they pin lived in
// the wiring, not in a unit.
package server

import (
	"context"
	"encoding/json"
	"io"
	"net"
	"net/http"
	"path/filepath"
	"strings"
	"testing"

	"github.com/calionauta/gogogo/config"
	"github.com/calionauta/gogogo/internal/auth"
	"github.com/calionauta/gogogo/internal/demo"
)

// bootDemoStack boots the production composition on an ephemeral port with
// the demo pair seeded, and returns the base URL plus a sign-in helper.
//
// rbac decides whether the policy is enforced: the access-denied test needs
// it on, the dashboard test does not care (the counts do not depend on it).
func bootDemoStack(t *testing.T, rbac bool) (base string, signIn func(email string) *http.Cookie) {
	t.Helper()
	cfg := config.Load()
	cfg.Dev = true
	cfg.SignupEnabled = true
	cfg.RBACEnabled = rbac
	cfg.DemoAccounts = true
	cfg.Turso.DBPath = filepath.Join(t.TempDir(), "stack.db")
	cfg.DataDir = filepath.Join(t.TempDir(), "data")
	cfg.NATS.Enabled = false
	cfg.DagNats.Enabled = false
	cfg.GoAkt.Enabled = false

	stack, err := BootChi(cfg)
	if err != nil {
		t.Fatalf("BootChi: %v", err)
	}
	ln, err := (&net.ListenConfig{}).Listen(context.Background(), "tcp", "127.0.0.1:0")
	if err != nil {
		t.Fatalf("listen: %v", err)
	}
	stop := serveChiOnListener(ln, srvHandler(stack.mux), stack)
	t.Cleanup(stop)
	base = "http://" + ln.Addr().String()

	return base, func(email string) *http.Cookie {
		t.Helper()
		body, err := json.Marshal(map[string]string{
			"credential": email,
			"password":   demo.Password,
		})
		if err != nil {
			t.Fatalf("marshal credentials: %v", err)
		}
		req, err := http.NewRequestWithContext(context.Background(), http.MethodPost,
			base+"/api/auth/signin/credential", strings.NewReader(string(body)))
		if err != nil {
			t.Fatalf("sign-in request: %v", err)
		}
		req.Header.Set("Content-Type", "application/json")
		resp, err := http.DefaultClient.Do(req)
		if err != nil {
			t.Fatalf("sign in: %v", err)
		}
		defer resp.Body.Close()
		if resp.StatusCode != http.StatusOK {
			b, _ := io.ReadAll(resp.Body)
			t.Fatalf("sign in status = %d (%s)", resp.StatusCode, strings.TrimSpace(string(b)))
		}
		for _, c := range resp.Cookies() {
			if c.Name == auth.CookieName && c.Value != "" {
				return c
			}
		}
		t.Fatalf("sign in set no %s cookie", auth.CookieName)
		return nil
	}
}
