// The dashboard's counts against the pages they summarize.
//
// The bug this pins was never in a query. It was in the scope: a count that
// filtered by owner while the page beside it listed every shared row, and
// a count whose table the recipe no longer owned — the query failed, the
// metric was dropped silently, and the card simply disappeared. Both
// looked like "the dashboard is empty for this account".
//
// So the test drives the real stack (the real declarations, the real
// registry) with two accounts and compares the numbers on the card with the
// rows the pages list.
package server

import (
	"context"
	"io"
	"net/http"
	"net/url"
	"regexp"
	"strings"
	"testing"

	"github.com/calionauta/gogogo/internal/demo"
)

// dashboardMetricRE pulls the (label, value) pair of every card metric out
// of the rendered page: the label div, then the odometer's aria-label.
var dashboardMetricRE = regexp.MustCompile(
	`<div class="text-xs uppercase tracking-wide text-base-content/50">([^<]*)</div>` +
		`\s*<div class="stat-value"[^>]*>\s*<span[^>]*aria-label="([^"]*)"`)

// dashboardMetrics returns the page's metrics in render order.
func dashboardMetrics(t *testing.T, body string) [][2]string {
	t.Helper()
	matches := dashboardMetricRE.FindAllStringSubmatch(body, -1)
	out := make([][2]string, 0, len(matches))
	for _, m := range matches {
		out = append(out, [2]string{m[1], m[2]})
	}
	return out
}

// getAs fetches one route as a signed-in user.
func getAs(t *testing.T, base string, cookie *http.Cookie, route string) string {
	t.Helper()
	req, err := http.NewRequestWithContext(context.Background(), http.MethodGet, base+route, nil)
	if err != nil {
		t.Fatalf("GET %s: %v", route, err)
	}
	req.AddCookie(cookie)
	resp, err := http.DefaultClient.Do(req)
	if err != nil {
		t.Fatalf("GET %s: %v", route, err)
	}
	defer resp.Body.Close()
	body, _ := io.ReadAll(resp.Body)
	if resp.StatusCode != http.StatusOK {
		t.Fatalf("GET %s = %d: %s", route, resp.StatusCode, strings.TrimSpace(string(body)))
	}
	return string(body)
}

// createTodo creates one todo through the API, with its visibility.
func createTodo(t *testing.T, base string, cookie *http.Cookie, title, visibility string) {
	t.Helper()
	form := url.Values{"title": {title}, "visibility": {visibility}, "filter": {"all"}}
	req, err := http.NewRequestWithContext(context.Background(), http.MethodPost,
		base+"/api/todos", strings.NewReader(form.Encode()))
	if err != nil {
		t.Fatalf("POST /api/todos: %v", err)
	}
	req.Header.Set("Content-Type", "application/x-www-form-urlencoded")
	req.AddCookie(cookie)
	resp, err := http.DefaultClient.Do(req)
	if err != nil {
		t.Fatalf("POST /api/todos: %v", err)
	}
	defer resp.Body.Close()
	if resp.StatusCode != http.StatusOK {
		body, _ := io.ReadAll(resp.Body)
		t.Fatalf("POST /api/todos = %d: %s", resp.StatusCode, strings.TrimSpace(string(body)))
	}
}

// createDoc opens the "new" surface of a shared document (notes, boards),
// which creates the row and answers with a redirect to it.
func createDoc(t *testing.T, base string, cookie *http.Cookie, route string) {
	t.Helper()
	req, err := http.NewRequestWithContext(context.Background(), http.MethodGet, base+route, nil)
	if err != nil {
		t.Fatalf("GET %s: %v", route, err)
	}
	req.AddCookie(cookie)
	client := &http.Client{CheckRedirect: func(_ *http.Request, _ []*http.Request) error {
		return http.ErrUseLastResponse
	}}
	resp, err := client.Do(req)
	if err != nil {
		t.Fatalf("GET %s: %v", route, err)
	}
	defer resp.Body.Close()
	if resp.StatusCode != http.StatusFound {
		t.Fatalf("GET %s = %d, want 302 (the new document)", route, resp.StatusCode)
	}
}

// TestDashboardCountsMatchThePages pins the dashboard against the pages it
// summarizes, for the account that used to see the wrong numbers.
//
// The data:
//
//	demo1: two todos (one public), one note, one board
//	demo2: one todo, one note
//
// demo2's cards must then read:
//
//	Todo      open 2, done 0, total 2, shared 1   (own + the public one)
//	Whiteboard total 1, mine 0                    (the board is demo1's)
//	Notes     total 2, mine 1                    (notes are shared)
//
// Every recipe that reports data keeps its card, and the link follows the
// role: a viewer cannot open the inbox or the chat, so those cards carry
// no button.
func TestDashboardCountsMatchThePages(t *testing.T) {
	base, signIn := bootDemoStack(t, true)
	admin := signIn(demo.Email)
	viewer := signIn(demo.Email2)

	createTodo(t, base, admin, "admin-private", "private")
	createTodo(t, base, admin, "admin-public", "public")
	createTodo(t, base, viewer, "viewer-private", "private")
	createDoc(t, base, admin, "/notes/new")
	createDoc(t, base, admin, "/whiteboard/new")
	createDoc(t, base, viewer, "/notes/new")

	// The pages the cards summarize, read the way the viewer reads them.
	// The marker each row repeats is the honest way to count them without
	// an HTML parser: a todo row carries data-created, a shared doc row
	// carries its link.
	todoRows := countRows(t, getAs(t, base, viewer, "/api/todos/fragment"), `data-created="`)
	noteRows := countRows(t, getAs(t, base, viewer, "/api/notes/fragment"), `href="/notes/`)
	boardRows := countRows(t, getAs(t, base, viewer, "/api/whiteboards/fragment"), `href="/whiteboard/`)

	page := getAs(t, base, viewer, "/dashboard")
	got := dashboardMetrics(t, page)

	want := [][2]string{
		{"Unread", "0"},
		{"Total", "0"},
		{"Open", "2"},
		{"Done", "0"},
		{"Total", "2"},
		{"Shared", "1"},
		{"Total", "1"},
		{"Mine", "0"},
		{"Total", "2"},
		{"Mine", "1"},
		{"Messages", "0"},
		{"Human", "0"},
	}
	if len(got) != len(want) {
		t.Fatalf("dashboard metrics = %v, want %v", got, want)
	}
	for i := range want {
		if got[i] != want[i] {
			t.Errorf("metric %d = %v, want %v", i, got[i], want[i])
		}
	}

	// The card's number is the number the page shows: the same counts, read
	// from the pages themselves.
	if todoRows != 2 {
		t.Errorf("todo page lists %d rows, the card says 2", todoRows)
	}
	if noteRows != 2 {
		t.Errorf("notes page lists %d rows, the card says 2", noteRows)
	}
	if boardRows != 1 {
		t.Errorf("whiteboard page lists %d rows, the card says 1", boardRows)
	}

	// Every recipe that reports data keeps its card, and the link follows
	// the role: a viewer cannot open the inbox or the chat.
	for _, want := range []string{"Inbox", "Todo", "Whiteboard", "Notes", "Chat"} {
		if !strings.Contains(page, ">"+want+"</div>") {
			t.Errorf("dashboard is missing the %q card", want)
		}
	}
	if strings.Contains(page, ">Open Inbox<") {
		t.Error("the viewer was offered the inbox, which the policy denies")
	}
	if strings.Contains(page, ">Open Chat<") {
		t.Error("the viewer was offered the chat, which the policy denies")
	}
	if !strings.Contains(page, ">Open Todo<") {
		t.Error("the viewer was not offered the todo, which the policy allows")
	}
}

// countRows counts the rows a fragment carries, by the marker each row
// repeats. The fragments are plain HTML lists, so the marker is the
// honest way to count them without an HTML parser.
func countRows(t *testing.T, fragment, marker string) int {
	t.Helper()
	return strings.Count(fragment, marker)
}
