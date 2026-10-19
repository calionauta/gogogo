// SCOPE:plugin — the Ask leg hydrates the app's own panels too.
package handlers

import (
	"context"
	"encoding/json"
	"maps"
	"os"
	"strings"
	"testing"

	comp "github.com/calionauta/gogogo/features/genui/components"
	"github.com/calionauta/gogogo/internal/queue"

	"github.com/a-h/templ"
)

// TestMain installs the stub panel so the renderer resolves "todo_list" —
// the registry is process state installed at boot, and this package's tests
// exercise the booted shape (see components/registry_test.go).
func TestMain(m *testing.M) {
	if err := comp.InstallPanelRegistry([]comp.PanelProvider{stubPanel{}}); err != nil {
		panic("genui handlers: test registry install failed: " + err.Error())
	}
	// Actions too: an answer naming an undeclared action is refused
	// (checkDeclaredAction), so a fixture emitting one has to declare it the way
	// a feature would. Process scope, hence TestMain.
	if aerr := comp.InstallActionRegistry([]comp.ActionProvider{stubActions{}}); aerr != nil {
		panic("genui handlers: test action registry install failed: " + aerr.Error())
	}
	os.Exit(m.Run())
}

// stubActions declares the actions the worker fixtures emit.
type stubActions struct{}

func (stubActions) Actions() []comp.ActionSpec {
	return []comp.ActionSpec{
		{
			Label: "Add Activity", Method: "POST", URL: "/add-activity",
			Fields: map[string]string{}, When: "the user wants an activity added to a plan",
		},
		{
			Label: "Apply", Method: "POST", URL: "/api/todos",
			Fields: map[string]string{"title": "the todo's text"},
			When:   "the user wants a task added",
		},
	}
}

// panelRowStub is this package's stub row shape. Deliberately local: the
// catalog seam is provider-based, so proving it must not require importing a
// feature package (that would invert the one-way dependency the seam keeps).
type panelRowStub struct {
	ID    string
	Title string
}

// panelViewStub renders the stub rows as a raw fragment.
func panelViewStub(filter string, rows []panelRowStub) templ.Component {
	var b strings.Builder
	b.WriteString(`<div data-filter="` + filter + `">`)
	for _, r := range rows {
		b.WriteString("<span>" + r.Title + "</span>")
	}
	b.WriteString("</div>")
	return templ.Raw(b.String())
}

// stubPanel is a panel provider with no store behind it.
type stubPanel struct{ rows []panelRowStub }

func (stubPanel) Component() string { return "todo_list" }

func (stubPanel) Prompt() string { return "todo_list {filter}" }

func (stubPanel) Writable() []string { return []string{"filter"} }

func (stubPanel) Render(props map[string]any) (templ.Component, error) {
	rows, _ := props["rows"].([]panelRowStub)
	filter, _ := props["filter"].(string)
	return panelViewStub(filter, rows), nil
}

func (s stubPanel) Fill(_ context.Context, _ string, props map[string]any) (map[string]any, error) {
	out := make(map[string]any, len(props)+1)
	maps.Copy(out, props)
	out["rows"] = s.rows
	return out, nil
}

// TestWorkerHydratesAppPanels pins the Ask leg: a panel is filled from its
// provider before delivery. Without this the panel would reach the renderer
// with no rows and draw an empty list — a lie ("you have no todos") about data
// the worker has.
//
// The "no provider" arm this test used to carry is gone by construction:
// Hydrate reads the installed registry, and InstallPanelRegistry refuses a
// panel with no provider, so an unwired panel is a failed boot rather than a
// delivered empty one. What remains to prove is that hydration HAPPENS, which
// is what this now asserts.
func TestWorkerHydratesAppPanels(t *testing.T) {
	t.Parallel()
	answer := `{"components":[{"type":"todo_list","props":{"filter":"open"}}]}`
	if err := comp.InstallPanelRegistry([]comp.PanelProvider{
		stubPanel{rows: []panelRowStub{{ID: "t1", Title: "real row"}}},
	}); err != nil {
		t.Fatalf("install provider: %v", err)
	}

	ownHub := queue.NewSSEHub()
	ch := make(chan []byte, 8)
	ownHub.Register("c1", "u1", ch)
	h := &Handler{hub: ownHub, responder: stubResponder{raw: answer}}
	payload, _ := json.Marshal(map[string]string{"prompt": "my todos", "userId": "u1"})
	_ = h.handleGenuiJob(context.Background(), nil, queue.Job{ClientID: "c1", Payload: payload})

	select {
	case msg := <-ch:
		if got := string(msg); !strings.Contains(got, "real row") {
			t.Errorf("payload lacks the hydrated row: %s", got)
		}
	default:
		t.Fatal("nothing was delivered")
	}
	ownHub.Unregister("c1")
}
