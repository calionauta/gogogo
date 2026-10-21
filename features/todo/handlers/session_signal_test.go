// SCOPE:feature — the shared session banner must only hear from the stream.
//
// This file exists because of a shipped bug: todo.Signals carried a
// SessionAuthed field, and every list/search/fragment response merges the
// WHOLE struct into the tab's signals. Handlers that have no session knowledge
// left the field at its zero value, so `sessionAuthed:false` rode every one of
// those responses — picking a different Match mode (or clicking a filter tab)
// showed "Session expired" to a perfectly signed-in user, and only a reload
// cleared it. The signal now lives on the stream handshake and its heartbeat,
// which are the two places that actually check the cookie.
package handlers

import (
	"context"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
)

// TestListSearchAndFragmentNeverPatchSessionAuthed is the assertion the bug
// needed and did not have: no per-page list response may speak about the
// session at all.
//
// RED-PROOFED: add a `SessionAuthed bool` back to todo.Signals (with the
// json tag) and every case below fails with `sessionAuthed` in the payload.
func TestListSearchAndFragmentNeverPatchSessionAuthed(t *testing.T) {
	t.Parallel()
	f := newHandlersFixture(t)
	cookie := f.signup("banner@example.com")

	for _, target := range []string{
		"/api/todos?filter=all&scope=all",
		"/api/todos/search?q=&filter=all&scope=all&mode=substring",
		"/api/todos/fragment?filter=all&scope=all",
	} {
		req := httptest.NewRequestWithContext(context.Background(), http.MethodGet, target, nil)
		req.AddCookie(cookie)
		rec := httptest.NewRecorder()
		f.r.ServeHTTP(rec, req)
		if rec.Code != http.StatusOK {
			t.Fatalf("GET %s: status = %d (body: %s)", target, rec.Code, rec.Body.String())
		}
		body := strings.ToLower(rec.Body.String())
		if strings.Contains(body, "sessionauthed") {
			t.Errorf("GET %s patches sessionAuthed (a list response cannot know the session; "+
				"a zero value reads as \"expired\" and raises the banner):\n%s", target, rec.Body.String())
		}
	}
}

// TestPageLevelResponsesNeverPatchConnectionSignals is the same invariant for
// the connection-owned trio. These are NOT cosmetic: a filter click used to
// merge `clientID:""` (every later mutation then went out with an empty id, so
// the tab no longer recognised its own broadcasts and worker feedback was
// addressed to a client that did not exist), `connectedClients:0` (the online
// pill dropped to "0 online" with a live stream) and `sidebarTab:""` (no pane
// matches it, so the right sidebar rendered blank).
//
// RED-PROOFED: restore `json:"clientID"` / `json:"connectedClients"` /
// `json:"sidebarTab"` on todo.Signals and every case fails.
func TestPageLevelResponsesNeverPatchConnectionSignals(t *testing.T) {
	t.Parallel()
	f := newHandlersFixture(t)
	cookie := f.signup("connection@example.com")

	for _, target := range []string{
		"/api/todos?filter=active&scope=all",
		"/api/todos/search?q=&filter=all&scope=all&mode=fuzzy",
		"/api/todos/fragment?filter=all&scope=all",
	} {
		req := httptest.NewRequestWithContext(context.Background(), http.MethodGet, target, nil)
		req.AddCookie(cookie)
		rec := httptest.NewRecorder()
		f.r.ServeHTTP(rec, req)
		if rec.Code != http.StatusOK {
			t.Fatalf("GET %s: status = %d (body: %s)", target, rec.Code, rec.Body.String())
		}
		body := strings.ToLower(rec.Body.String())
		// The behavioural ones (identity, presence, sidebar) plus the visible
		// ones (date filter, demo steppers, inline edit) — one list because one
		// invariant: a page-level response may not write browser-owned signals.
		for _, key := range []string{
			"\"clientid\":", "\"connectedclients\":", "\"sidebartab\":",
			"\"createdafter\":", "\"realtimekind\":", "\"techstep\":",
			"\"demostep\":", "\"aistep\":", "\"suggestions\":",
			"\"onboardingstep\":", "\"workflowcompleted\":", "\"editingid\":",
			"\"lastitemsource\":", "\"permdenied\":",
		} {
			if strings.Contains(body, key) {
				t.Errorf("GET %s patches %s — a page-level response cannot know the tab's "+
					"connection state (see todo.Signals):\n%s", target, key, rec.Body.String())
			}
		}
	}
}
