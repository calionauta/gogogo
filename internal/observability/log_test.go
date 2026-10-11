// SCOPE:core — Tests for the observability core
package observability

import (
	"bytes"
	"context"
	"log/slog"
	"strings"
	"testing"
)

func TestLIsNonNil(t *testing.T) {
	if L() == nil {
		t.Fatal("expected base logger")
	}
}

// TestWithFieldsPropagates proves WithFields actually enriches the logger
// carried on the context — not just that a function returns without panic.
func TestWithFieldsPropagates(t *testing.T) {
	var buf bytes.Buffer
	handler := slog.NewJSONHandler(&buf, nil)
	ctx := context.WithValue(context.Background(), loggerKey, slog.New(handler))

	ctx = WithFields(ctx, map[string]any{"request_id": "abc", "user": "u1"})
	From(ctx).Info("hello")

	out := buf.String()
	if !strings.Contains(out, `"request_id":"abc"`) {
		t.Fatalf("expected request_id in log output, got: %s", out)
	}
	if !strings.Contains(out, `"user":"u1"`) {
		t.Fatalf("expected user in log output, got: %s", out)
	}
}

// TestWithComponentShape pins the attribute pairs every call site relies on.
func TestWithComponentShape(t *testing.T) {
	got := WithComponent("backend.custom", "core")
	want := []any{"component", "backend.custom", "layer", "core"}
	if len(got) != len(want) {
		t.Fatalf("WithComponent len = %d, want %d", len(got), len(want))
	}
	for i := range want {
		if got[i] != want[i] {
			t.Fatalf("WithComponent[%d] = %v, want %v", i, got[i], want[i])
		}
	}
}
