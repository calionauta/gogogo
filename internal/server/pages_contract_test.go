// SCOPE:core — Page contracts and structure digests for every route.
//
// Phase 0 of the UI revamp (see tmp/gogogo-ui-revamp/implementation-plan.md).
// The revamp rewrites every page template; the existing render tests assert
// substrings, which a rewrite breaks *around* without failing. This test pins
// two things per route, both reviewable as a diff:
//
//  1. CONTRACT — the ids, attributes and copy the wire depends on: patch
//     roots (D5), client-owned signal bindings (D4), the aria-live region,
//     and the streaming empty-state sentence.
//  2. DIGEST — a canonical, sorted *shape* of the page: id attributes,
//     data-on events, $signal names, tag histogram, control counts, aria
//     attributes. It counts tag-like text in comments and script strings too
//     (no HTML parser here) — a bare "<script>" written in a .templ comment
//     moves the histogram, which is how one such comment got caught.
//     Deliberately NOT the full HTML: a whole-page golden would
//     be ~500 KB of churn that a deliberate redesign re-blesses on every
//     phase, which trains reviewers to ignore the file.
//
// Update the digests deliberately, and read the diff first:
//
//	PAGES_DIGEST=update go test ./internal/server -run TestPagesDigest
package server

import (
	"context"
	"encoding/json"
	"fmt"
	"io"
	"net"
	"net/http"
	"os"
	"path/filepath"
	"regexp"
	"sort"
	"strings"
	"testing"

	"github.com/calionauta/gogogo/config"
	"github.com/calionauta/gogogo/internal/auth"
	"github.com/calionauta/gogogo/internal/demo"
)

// pageUnderTest is one route plus the contract strings it must keep serving.
type pageUnderTest struct {
	name  string // digest file name
	route string
	anon  bool     // fetch without a session (landing, login)
	must  []string // substrings that must survive the revamp
}

// pagesUnderTest is the revamp's contract table. Every entry names something
// a caller depends on, not something a designer likes: a patch root, a signal
// binding, an aria contract, or copy a render test already asserts.
var pagesUnderTest = []pageUnderTest{
	{name: "landing", route: "/", anon: true, must: []string{"Access demo site"}},
	{name: "login", route: "/login", anon: true, must: []string{"Email", demo.Email}},
	{name: "todo", route: "/todo", must: []string{
		`id="todo-list"`,
		`data-bind="newTitle"`,
		`id="suggestions-region"`,
		`id="toast-container"`,
	}},
	{name: "chat", route: "/chat", must: []string{
		`id="chat-composer"`,
		`for="chat-prompt"`,
		`id="chat-messages"`,
		`aria-live="polite"`,
		"Ask anything — answers stream below",
	}},
	{name: "review", route: "/review", must: []string{
		// The composer (review-composer / review-tab / review-content) is gated
		// by signals.ReviewEnabled, which needs a real provider key; a stock
		// boot (empty GOAI_API_KEY) renders the not-configured shape. Both
		// states are legitimate, and this boot pins the second one — the
		// enabled composer is covered by features/review/components tests.
		`id="review-result"`,
	}},
	{name: "room", route: "/room", must: []string{
		`id="room-roster"`,
		`id="room-presenter"`,
		`id="room-generation"`,
		`id="room-acquire"`,
		`id="room-release"`,
		`id="room-leave"`,
		`id="room-crash"`,
	}},
	// net-status lives on the DOC page (/notes/{id}), not the list: the list
	// only carries notes-list + the resync affordance.
	{name: "notes", route: "/notes", must: []string{`id="notes-list"`}},
	{name: "whiteboard", route: "/whiteboard", must: []string{`id="whiteboard-list"`}},
	{name: "account", route: "/account", must: []string{
		"Sessions",
		"Two-factor authentication",
	}},
	{name: "config", route: "/config", must: []string{
		"Runtime view + admin actions.",
		"Access control (Casbin).",
		"Users",
		"Security events",
		"<th class=\"w-1/3\">Field</th>",
	}},
}

// bootPages boots the production composition on an ephemeral port and signs
// the admin demo account in, so every route is fetched the way a real session
// fetches it (cookie, RBAC, skin resolution).
func bootPages(t *testing.T) (base string, cookie *http.Cookie) {
	t.Helper()
	cfg := config.Load()
	cfg.Dev = true
	cfg.SignupEnabled = true
	cfg.RBACEnabled = false
	cfg.DemoAccounts = true
	tmp := t.TempDir()
	cfg.Turso.DBPath = filepath.Join(tmp, "pages.db")
	cfg.DataDir = filepath.Join(tmp, "data")
	cfg.NATS.Enabled = false
	cfg.DagNats.Enabled = false
	// GoAkt stays ON: /room's roster, presenter lock and generation counter
	// are in-process grains, and a disabled engine would leave the route
	// unmounted (a 404 that looks like a template failure).
	cfg.GoAkt.Enabled = true

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

	body, err := json.Marshal(map[string]string{
		"credential": demo.Email,
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
			return base, c
		}
	}
	t.Fatalf("sign in set no %s cookie", auth.CookieName)
	return "", nil
}

// fetchPage GETs one route and returns the body, failing on a non-200.
func fetchPage(t *testing.T, base string, c *http.Cookie, route string) string {
	t.Helper()
	req, err := http.NewRequestWithContext(context.Background(), http.MethodGet, base+route, nil)
	if err != nil {
		t.Fatalf("GET %s: %v", route, err)
	}
	if c != nil {
		req.AddCookie(c)
	}
	resp, err := http.DefaultClient.Do(req)
	if err != nil {
		t.Fatalf("GET %s: %v", route, err)
	}
	defer resp.Body.Close()
	body, err := io.ReadAll(resp.Body)
	if err != nil {
		t.Fatalf("GET %s: read body: %v", route, err)
	}
	// A trimmed build (installer presets drop features) legitimately does not
	// mount every route, and this package's tests run inside those trimmed
	// trees. Ownership of "the route still exists" is the installer's own
	// suite; here a missing route just means there is nothing to pin.
	if resp.StatusCode == http.StatusNotFound {
		t.Skipf("%s is not mounted in this build (trimmed feature)", route)
	}
	if resp.StatusCode != http.StatusOK {
		t.Fatalf("GET %s status = %d, want 200 (body: %.200s)", route, resp.StatusCode, body)
	}
	return string(body)
}

// TestPagesContract fails when a route stops serving what callers depend on.
func TestPagesContract(t *testing.T) {
	base, cookie := bootPages(t)
	for _, p := range pagesUnderTest {
		t.Run(p.name, func(t *testing.T) {
			c := cookie
			if p.anon {
				c = nil
			}
			body := fetchPage(t, base, c, p.route)
			for _, want := range p.must {
				if !strings.Contains(body, want) {
					t.Errorf("%s: missing %q (a revamp changed a contract — "+
						"either restore it or update this table and the test that pins it)", p.route, want)
				}
			}
		})
	}
}

var (
	rePageID   = regexp.MustCompile(`\sid="([^"]*)"`)
	rePageBind = regexp.MustCompile(`data-bind="([^"]*)"`)
	rePageOn   = regexp.MustCompile(`\s(data-on:[a-z:]+)=`)
	rePageSig  = regexp.MustCompile(`\$([a-zA-Z_][a-zA-Z0-9_]*)`)
	rePageAria = regexp.MustCompile(`\s(aria-[a-z]+)=`)
	rePageTag  = regexp.MustCompile(`<([a-z][a-z0-9]*)`)
	rePageUUID = regexp.MustCompile(`[0-9a-f]{8}-[0-9a-f]{4}-[0-9a-f]{4}-[0-9a-f]{4}-[0-9a-f]{12}`)
)

// digestPage renders the canonical shape of a page: stable, sorted, and small
// enough that a diff points at the element that changed.
func digestPage(route, body string) string {
	uniq := func(re *regexp.Regexp) []string {
		seen := map[string]bool{}
		var out []string
		for _, m := range re.FindAllStringSubmatch(body, -1) {
			v := rePageUUID.ReplaceAllString(m[1], "<uuid>")
			if !seen[v] {
				seen[v] = true
				out = append(out, v)
			}
		}
		sort.Strings(out)
		return out
	}
	tags := map[string]int{}
	for _, m := range rePageTag.FindAllStringSubmatch(body, -1) {
		tags[m[1]]++
	}
	tagNames := make([]string, 0, len(tags))
	for name := range tags {
		tagNames = append(tagNames, name)
	}
	sort.Strings(tagNames)

	var b strings.Builder
	fmt.Fprintf(&b, "route %s\n", route)
	fmt.Fprintf(&b, "ids %s\n", strings.Join(uniq(rePageID), " "))
	fmt.Fprintf(&b, "data-bind %s\n", strings.Join(uniq(rePageBind), " "))
	fmt.Fprintf(&b, "data-on %s\n", strings.Join(uniq(rePageOn), " "))
	fmt.Fprintf(&b, "signals %s\n", strings.Join(uniq(rePageSig), " "))
	fmt.Fprintf(&b, "aria %s\n", strings.Join(uniq(rePageAria), " "))
	tagParts := make([]string, 0, len(tagNames))
	for _, name := range tagNames {
		tagParts = append(tagParts, fmt.Sprintf("%s=%d", name, tags[name]))
	}
	fmt.Fprintf(&b, "tags %s\n", strings.Join(tagParts, " "))
	return b.String()
}

// TestPagesDigest compares each route's canonical shape with the committed
// digest. Review the diff; re-bless only what you meant to change.
func TestPagesDigest(t *testing.T) {
	base, cookie := bootPages(t)

	// The digests pin the FULL composition (all ten modules, both skins'
	// assets). Installer presets run these tests inside trimmed trees, where a
	// missing route or a dropped nav link is the point, not a regression — so a
	// build that does not mount every route is not the build these goldens
	// describe. Ownership of "the trim removed exactly what it promised" is
	// internal/installer's suite.
	for _, p := range pagesUnderTest {
		if !mounted(t, base, cookie, p) {
			t.Skipf("trimmed build: %s is not mounted, digests describe the full composition", p.route)
		}
	}
	update := os.Getenv("PAGES_DIGEST") == "update"
	dir := filepath.Join("testdata", "pages")
	if update {
		if err := os.MkdirAll(dir, 0o750); err != nil {
			t.Fatalf("mkdir %s: %v", dir, err)
		}
	}
	for _, p := range pagesUnderTest {
		c := cookie
		if p.anon {
			c = nil
		}
		got := digestPage(p.route, fetchPage(t, base, c, p.route))
		path := filepath.Join(dir, p.name+".digest")
		if update {
			if err := os.WriteFile(path, []byte(got), 0o600); err != nil {
				t.Fatalf("write %s: %v", path, err)
			}
			continue
		}
		want, err := os.ReadFile(path)
		if err != nil {
			t.Fatalf("read %s: %v (run with PAGES_DIGEST=update to create it)", path, err)
		}
		if string(want) != got {
			t.Errorf("%s: structure digest changed —\n%s\n(re-bless intentionally: "+digestBlessCmd+")",
				p.route, diffLines(string(want), got))
		}
	}
}

// digestBlessCmd is printed by the digest failure so the fix is copy-pasteable
// without hunting this file.
const digestBlessCmd = "PAGES_DIGEST=update go test ./internal/server -run TestPagesDigest"

// mounted reports whether a route exists in this build.
func mounted(t *testing.T, base string, c *http.Cookie, p pageUnderTest) bool {
	t.Helper()
	if p.anon {
		c = nil
	}
	req, err := http.NewRequestWithContext(context.Background(), http.MethodGet, base+p.route, nil)
	if err != nil {
		return false
	}
	if c != nil {
		req.AddCookie(c)
	}
	resp, err := http.DefaultClient.Do(req)
	if err != nil {
		return false
	}
	defer resp.Body.Close()
	_, _ = io.Copy(io.Discard, resp.Body)
	return resp.StatusCode != http.StatusNotFound
}

// diffLines reports the lines that differ between two digests, prefixed -/+.
func diffLines(want, got string) string {
	w, g := strings.Split(want, "\n"), strings.Split(got, "\n")
	var b strings.Builder
	for i := 0; i < len(w) || i < len(g); i++ {
		var wl, gl string
		if i < len(w) {
			wl = w[i]
		}
		if i < len(g) {
			gl = g[i]
		}
		if wl == gl {
			continue
		}
		if wl != "" {
			fmt.Fprintf(&b, "-%s\n", wl)
		}
		if gl != "" {
			fmt.Fprintf(&b, "+%s\n", gl)
		}
	}
	return b.String()
}

// TestPrivatePagesRejectAnonymous pins the handler-level session check that
// RBAC cannot provide when it is off: every page marked private in the
// contract table must bounce an anonymous request (303/401/403), never render.
func TestPrivatePagesRejectAnonymous(t *testing.T) {
	base, _ := bootPages(t)
	for _, p := range pagesUnderTest {
		if p.anon {
			continue
		}
		t.Run(p.name, func(t *testing.T) {
			req, err := http.NewRequestWithContext(context.Background(), http.MethodGet, base+p.route, nil)
			if err != nil {
				t.Fatalf("request: %v", err)
			}
			client := &http.Client{CheckRedirect: func(_ *http.Request, _ []*http.Request) error {
				return http.ErrUseLastResponse
			}}
			resp, err := client.Do(req)
			if err != nil {
				t.Fatalf("GET %s: %v", p.route, err)
			}
			defer resp.Body.Close()
			if resp.StatusCode == http.StatusOK {
				t.Fatalf("anonymous GET %s rendered 200", p.route)
			}
		})
	}
}
