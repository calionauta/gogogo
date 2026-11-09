// Odo render and signal contracts.
package components

import (
	"bytes"
	"context"
	"strings"
	"testing"

	"github.com/a-h/templ"

	"github.com/calionauta/gogogo/internal/ui"
)

func renderOdo(t *testing.T, value int, signal string) string {
	t.Helper()
	ctx := context.Background()
	ctx = templ.WithNonce(ctx, "test-nonce-0123456789")
	ctx = ui.WithInboxUnread(ctx, 0)
	var buf bytes.Buffer
	if err := Odo(value, signal).Render(ctx, &buf); err != nil {
		t.Fatalf("render Odo: %v", err)
	}
	return buf.String()
}

func TestOdoRendersFinalValue(t *testing.T) {
	html := renderOdo(t, 1284, "openTodos")
	// The effect names the element with the expression's own `el` (the one
	// variable Datastar binds beside $-signals) — never $el, which the
	// engine would rewrite to $['el'] (a signal named "el").
	for _, want := range []string{
		`class="odo"`,
		`data-effect="window.__gogogoOdo(el, $openTodos)"`,
		`aria-label="1284"`,
		"translateY(-1em)",
		"translateY(-2em)",
		"translateY(-8em)",
		"translateY(-4em)",
	} {
		if !strings.Contains(html, want) {
			t.Fatalf("Odo(1284) missing %q in %s", want, html)
		}
	}
	if got := strings.Count(html, `class="odo-digit"`); got != 4 {
		t.Fatalf("Odo(1284) has %d odo-digit slots, want 4", got)
	}
}

func TestOdoZeroIsSingleDigit(t *testing.T) {
	html := renderOdo(t, 0, "openTodos")
	if !strings.Contains(html, `aria-label="0"`) {
		t.Fatalf("Odo(0) missing aria-label in %s", html)
	}
	if got := strings.Count(html, `class="odo-digit"`); got != 1 {
		t.Fatalf("Odo(0) has %d odo-digit slots, want 1", got)
	}
	if !strings.Contains(html, "translateY(-0em)") {
		t.Fatalf("Odo(0) missing translateY(-0em) in %s", html)
	}
}

func TestOdoSingleDigit(t *testing.T) {
	html := renderOdo(t, 7, "inboxUnread")
	if !strings.Contains(html, `data-effect="window.__gogogoOdo(el, $inboxUnread)"`) {
		t.Fatalf("Odo(7) missing inbox signal effect in %s", html)
	}
	if got := strings.Count(html, `class="odo-digit"`); got != 1 {
		t.Fatalf("Odo(7) has %d odo-digit slots, want 1", got)
	}
}

func TestMarshalSignalsCarriesOpenTodos(t *testing.T) {
	got := MarshalSignals(Signals{OpenTodos: 12})
	if !strings.Contains(got, `"openTodos":12`) {
		t.Fatalf("MarshalSignals = %s, want openTodos:12", got)
	}
}
