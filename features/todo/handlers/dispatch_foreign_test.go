// SCOPE:feature — HTTP todo foreign-envelope isolation red-proof.
//
// All features share one hub, so worker results addressed to other
// features' tabs (genui/chat broadcasts without a clientID) also cross
// the todo stream. The dispatcher must swallow them: merging a foreign
// result as lastJob overwrites the debug signal with noise.
package handlers

import (
	"encoding/json"
	"net/http/httptest"
	"testing"

	"github.com/calionauta/gogogo/internal/queue"
)

// TestDispatchIgnoresForeignResults pins that chat result envelopes are
// consumed silently (handled, no error) instead of falling through to
// the lastJob debug merge.
func TestDispatchIgnoresForeignResults(t *testing.T) {
	f := newSuggestFixture(t, false, false)
	for _, typ := range []string{"chat"} {
		body, err := json.Marshal(queue.Job{Type: typ, Payload: json.RawMessage(`{}`)})
		if err != nil {
			t.Fatalf("marshal %s envelope: %v", typ, err)
		}
		var job queue.Job
		if uErr := json.Unmarshal(body, &job); uErr != nil {
			t.Fatalf("decode %s envelope: %v", typ, uErr)
		}
		rec := httptest.NewRecorder()
		handled, err := f.h.dispatchJob(sseGen(t, rec), job)
		if err != nil {
			t.Fatalf("dispatch %s: %v", typ, err)
		}
		if !handled {
			t.Fatalf("dispatch %s: not handled (falls through to lastJob noise)", typ)
		}
		if out := rec.Body.String(); out != "" {
			t.Fatalf("dispatch %s must emit nothing, got:\n%s", typ, out)
		}
	}
}
