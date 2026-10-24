// SCOPE:feature — chat surface + hydration contracts (red-proof).
//
// Per-request answer surfaces ride the ask job; history re-hydrates panels
// from the store. Each test fails when its behavior is reverted.
package handlers

import (
	"context"
	"html"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
	"time"

	"github.com/calionauta/gogogo/features/chat/store"
	comp "github.com/calionauta/gogogo/features/genui/components"
)

// TestHandlersChatAskRejectsUnknownSurfaces pins the fail-closed ask path: a
// surfaces value outside the registry is a 400, never a silently degraded
// vocabulary. The composer only sends names it toggles, so this fires on a
// hand-made request, not on UI traffic.
func TestHandlersChatAskRejectsUnknownSurfaces(t *testing.T) {
	mux, _, _ := chatFixture(t, stubResponder{raw: "{}"})
	cookie := chatSignup(t, mux, "surfaces-bad@example.com")

	rec := httptest.NewRecorder()
	req := httptest.NewRequestWithContext(context.Background(),
		http.MethodPost, "/api/chat/ask?clientID=bad-t1&surfaces=bogus",
		strings.NewReader("prompt=hi"))
	req.Header.Set("Content-Type", "application/x-www-form-urlencoded")
	req.AddCookie(cookie)
	mux.ServeHTTP(rec, req)
	if rec.Code != http.StatusBadRequest {
		t.Fatalf("ask status = %d, want 400 for an unknown surface", rec.Code)
	}
}

// TestHandlersChatSurfacesRideTheJob is the end-to-end toggle contract: the
// tab turns every band off (surfaces=), the worker degrades the invented
// visual to an honest line instead of rendering a band the tab disabled.
// Reverting the worker to the handler default drops this into the raw
// html_block and fails the test.
func TestHandlersChatSurfacesRideTheJob(t *testing.T) {
	invented := `{"components":[{"type":"html_block","props":{"html":"<div class='card'><p>hi</p></div>"}}]}`
	mux, a, mem := chatFixture(t, stubResponder{raw: invented})
	cookie := chatSignup(t, mux, "surfaces@example.com")

	rec := httptest.NewRecorder()
	req := httptest.NewRequestWithContext(context.Background(),
		http.MethodPost, "/api/chat/ask?clientID=surf-t1&surfaces=",
		strings.NewReader("prompt=hi"))
	req.Header.Set("Content-Type", "application/x-www-form-urlencoded")
	req.AddCookie(cookie)
	mux.ServeHTTP(rec, req)
	if rec.Code != http.StatusOK {
		t.Fatalf("ask status = %d (%s)", rec.Code, rec.Body.String())
	}

	owner := sessionOwner(t, a, cookie)
	deadline := time.Now().Add(8 * time.Second)
	for {
		msgs, _ := mem.List(context.Background(), owner)
		if len(msgs) == 2 {
			got := msgs[1]
			if len(got.Components) != 1 || got.Components[0].Component != "text_note" {
				t.Fatalf("all-off answer = %+v, want the honest text_note fallback", got.Components)
			}
			return
		}
		if time.Now().After(deadline) {
			t.Fatal("worker never answered")
		}
		time.Sleep(100 * time.Millisecond)
	}
}

// TestHandlersChatHistoryHydratesPanels pins reload freshness: a stored
// panel directive (model-chosen slice, no rows) renders with the viewer's
// CURRENT store rows — not the snapshot from answer time. Revert messageViews
// to render stored dirs directly and the fresh row is missing.
func TestHandlersChatHistoryHydratesPanels(t *testing.T) {
	panels := []comp.PanelProvider{stubPanel{rows: []panelRowStub{{ID: "t1", Title: "fresh row"}}}}
	mux, a, mem := chatFixtureWith(t, stubResponder{raw: "{}"}, panels)
	cookie := chatSignup(t, mux, "hydrate@example.com")
	owner := sessionOwner(t, a, cookie)

	ctx := context.Background()
	if err := mem.Append(ctx, owner, store.Message{Role: "assistant", Components: []comp.Directive{{
		Component: "todo_list", Props: map[string]any{"filter": "open"},
	}}}); err != nil {
		t.Fatal(err)
	}

	rec := httptest.NewRecorder()
	req := httptest.NewRequestWithContext(ctx, http.MethodGet, "/chat", nil)
	req.AddCookie(cookie)
	mux.ServeHTTP(rec, req)
	if rec.Code != http.StatusOK {
		t.Fatalf("GET /chat status = %d", rec.Code)
	}
	if got := html.UnescapeString(rec.Body.String()); !strings.Contains(got, "fresh row") {
		t.Errorf("/chat did not hydrate the stored panel (got %.400q…)", got)
	}
}
