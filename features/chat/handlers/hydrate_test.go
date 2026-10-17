// SCOPE:feature — the system-panel hydration path in chat.
package handlers

import (
	"context"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
	"time"

	comp "github.com/calionauta/gogogo/features/genui/components"
)

// TestChatHydratesSystemPanel pins the whole point of a system component:
// the answer asks for a slice of the app's OWN state and the worker fills it
// from the store, so the bubble carries the viewer's real rows rather than
// anything the model wrote. Red-proof: drop the Hydrate call from the worker
// and the stored directive keeps empty rows.
func TestChatHydratesSystemPanel(t *testing.T) {
	answer := `{"components":[{"type":"todo_list","props":{"filter":"open"}}]}`
	panels := []comp.PanelProvider{stubPanel{rows: []panelRowStub{{ID: "t1", Title: "real row"}}}}
	mux, a, mem := chatFixtureWith(t, stubResponder{raw: answer}, panels)
	cookie := chatSignup(t, mux, "panel@example.com")

	rec := httptest.NewRecorder()
	req := httptest.NewRequestWithContext(context.Background(),
		http.MethodPost, "/api/chat/ask?clientID=panel-t1",
		strings.NewReader("prompt=what are my open todos"))
	req.Header.Set("Content-Type", "application/x-www-form-urlencoded")
	req.AddCookie(cookie)
	mux.ServeHTTP(rec, req)
	if rec.Code != http.StatusOK {
		t.Fatalf("ask status = %d", rec.Code)
	}

	owner := sessionOwner(t, a, cookie)
	deadline := time.Now().Add(8 * time.Second)
	for {
		msgs, err := mem.List(context.Background(), owner)
		if err == nil && len(msgs) == 2 && len(msgs[1].Components) == 1 {
			assertPanelRow(t, msgs[1].Components[0])
			return
		}
		if time.Now().After(deadline) {
			t.Fatalf("worker never answered (msgs=%+v err=%v)", msgs, err)
		}
		time.Sleep(100 * time.Millisecond)
	}
}

func assertPanelRow(t *testing.T, dir comp.Directive) {
	t.Helper()
	if dir.Component != "todo_list" {
		t.Fatalf("component = %q, want the panel", dir.Component)
	}
	rows, ok := dir.Props["rows"].([]panelRowStub)
	if !ok || len(rows) != 1 || rows[0].Title != "real row" {
		t.Fatalf("rows = %#v, want the hydrated store row", dir.Props["rows"])
	}
}
