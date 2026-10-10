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

// TestSuggestCopyAndContext pins the Suggest panel contract against the
// live-demo bug report: provider copy must never name a hardcoded
// provider (it follows env), the panel must link the Ask page (Suggest
// returns title strings, Ask renders components — users confused the
// two), and new suggestions must pull the viewport to them
// (data-effect scroll: on narrow screens the region lands below the
// fold and the click gives no feedback).
func TestSuggestCopyAndContext(t *testing.T) {
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

	for _, stale := range []string{"Groq via GoAI", "configured LLM (Groq)"} {
		if strings.Contains(html, stale) {
			t.Errorf("page names a hardcoded provider %q (follows env, not copy)", stale)
		}
	}
	for _, want := range []string{
		`href="/genui"`,
		`data-effect="window.__gogogoSuggestScroll($suggestions)"`,
		`__gogogoSuggestScroll = function`,
	} {
		if !strings.Contains(html, want) {
			t.Errorf("page missing contextual Suggest wiring %q", want)
		}
	}
}
