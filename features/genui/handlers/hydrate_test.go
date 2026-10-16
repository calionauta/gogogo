// SCOPE:plugin — the Ask leg hydrates the app's own panels too.
package handlers

import (
	"context"
	"encoding/json"
	"maps"
	"strings"
	"testing"

	comp "github.com/calionauta/gogogo/features/genui/components"
	"github.com/calionauta/gogogo/internal/queue"
)

// stubPanel is a panel provider with no store behind it.
type stubPanel struct{ rows []comp.TodoRow }

func (stubPanel) Component() string { return "todo_list" }

func (s stubPanel) Fill(_ context.Context, _ string, props map[string]any) (map[string]any, error) {
	out := make(map[string]any, len(props)+1)
	maps.Copy(out, props)
	out["rows"] = s.rows
	return out, nil
}

// TestWorkerHydratesAppPanels pins the Ask leg: a registered panel is filled
// from the provider before delivery, and dropped when no provider is wired.
// Without this the panel would reach the renderer with no rows and draw an
// empty list — a lie ("you have no todos") about data the worker has.
func TestWorkerHydratesAppPanels(t *testing.T) {
	t.Parallel()
	answer := `{"components":[{"type":"todo_list","props":{"filter":"open"}}]}`
	for name, tc := range map[string]struct {
		panels []comp.PanelProvider
		want   string
	}{
		"hydrated": {
			panels: []comp.PanelProvider{stubPanel{rows: []comp.TodoRow{{ID: "t1", Title: "real row"}}}},
			want:   "real row",
		},
		"no provider": {want: ""},
	} {
		ownHub := queue.NewSSEHub()
		ch := make(chan []byte, 8)
		ownHub.Register("c1", "u1", ch)
		h := &Handler{hub: ownHub, responder: stubResponder{raw: answer}, panels: tc.panels}
		payload, _ := json.Marshal(map[string]string{"prompt": "my todos", "userId": "u1"})
		_ = h.handleGenuiJob(context.Background(), nil, queue.Job{ClientID: "c1", Payload: payload})

		select {
		case msg := <-ch:
			got := string(msg)
			if tc.want != "" && !strings.Contains(got, tc.want) {
				t.Errorf("%s: payload lacks the hydrated row: %s", name, got)
			}
			if tc.want == "" && strings.Contains(got, "todo_list") {
				t.Errorf("%s: an unhydrated panel was delivered: %s", name, got)
			}
		default:
			t.Fatalf("%s: nothing was delivered", name)
		}
		ownHub.Unregister("c1")
	}
}
