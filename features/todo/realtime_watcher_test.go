package todo_test

import (
	"context"
	"io"
	"net/http"
	"net/http/cookiejar"
	"strings"
	"testing"
	"time"
)

// TestDocVersionWatcherIsReactive pins the reactive watcher contract:
// the doc-version resync must ride a data-effect subscription on
// $docVersion, not a 250ms setInterval poll. Polling kept one timer
// alive per tab forever and re-read the DOM signal attribute on every
// tick; the effect fires only when the signal actually changes (plus
// once on load, which the watcher skips via first-run guard).
func TestDocVersionWatcherIsReactive(t *testing.T) {
	t.Parallel()
	base, _, _, _, cleanup := testFixture(t)
	defer cleanup()
	ctx := context.Background()

	jar, _ := cookiejar.New(nil)
	client := &http.Client{Jar: jar, Timeout: 15 * time.Second}
	loginUser(ctx, t, client, base, demoEmail, demoPassword)

	req, err := http.NewRequestWithContext(ctx, http.MethodGet, base+"/todo", nil)
	if err != nil {
		t.Fatalf("request: %v", err)
	}
	resp, err := client.Do(req)
	if err != nil {
		t.Fatalf("GET /todo: %v", err)
	}
	defer resp.Body.Close()
	body, _ := io.ReadAll(resp.Body)
	html := string(body)

	for _, want := range []string{
		`data-effect="window.__gogogoDocWatch($docVersion)"`,
		`__gogogoDocWatch = function`,
	} {
		if !strings.Contains(html, want) {
			t.Errorf("page missing reactive watcher %q", want)
		}
	}
	if strings.Contains(html, "setInterval(watchDocVersion") {
		t.Errorf("page still polls docVersion on a 250ms timer — the effect replaces it")
	}
}
