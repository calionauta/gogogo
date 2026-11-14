// Keyless demo answer and responder selection.
package components

import (
	"context"
	"strings"
	"testing"

	"github.com/calionauta/gogogo/internal/llm"
)

// TestResponderSelection pins the one thing left to choose: no real model is a
// fail-closed error naming the knob. There is no canned answer any more — a
// fixed grid for every prompt looked configured and answered the wrong question,
// and the page now renders the shared setup notice instead.
func TestResponderSelection(t *testing.T) {
	t.Parallel()
	ctx := context.Background()
	_, err := NewResponder(nil, nil, nil).Respond(ctx, "plan my day")
	if err == nil {
		t.Fatal("no model answered anyway; a canned answer must not come back")
	}
	if !strings.Contains(err.Error(), "GOAI_API_KEY") {
		t.Errorf("without a key = %v, want the setup error naming the knob", err)
	}
	// A real client with no key set is still "not configured": the client
	// exists, the credential does not.
	_, err = NewResponder(llm.New(""), nil, nil).Respond(ctx, "plan my day")
	if err == nil {
		t.Fatal("an empty key was treated as a configured model")
	}
}
