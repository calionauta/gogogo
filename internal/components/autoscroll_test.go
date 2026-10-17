// SCOPE:core — AutoScroll render + expression contract test.
package components_test

import (
	"bytes"
	"context"
	"os"
	"os/exec"
	"path/filepath"
	"strconv"
	"strings"
	"testing"

	"github.com/calionauta/gogogo/internal/components"
)

// TestAutoScrollExprCompiles is the red-proof for the scroll fix's one real
// failure mode: the attribute value is not an HTML attribute, it is a
// JavaScript function BODY that Datastar compiles with its own rewrite
// (`Function(...names, expr)`, plus a return-wrap for value-returning
// attributes). A malformed body is a compile error the browser reports as a
// generic Datastar error while the scroll silently never runs — invisible to
// every Go test and to `node --check` alike.
//
// The assertion rebuilds exactly what Datastar builds (`Function('el', expr)`
// — the same constructor) and EXECUTES it in node, so the test fails on
// precisely what the browser fails on. Skipped — visibly — when node is
// unavailable, instead of passing vacuously: a silent skip here would read
// as coverage that does not exist. (CI installs node for the Playwright
// suites, so the gate does run there.)
func TestAutoScrollExprCompiles(t *testing.T) {
	t.Parallel()
	node, err := exec.LookPath("node")
	if err != nil {
		t.Skip("node not on PATH: cannot compile-check the Datastar expression")
	}
	// Datastar's own compile step, in the same constructor: `new Function(
	// 'el', body)`. Using new Function rather than a syntax check of a
	// re-wrapped file is the point: it is the same constructor, so it
	// accepts and rejects exactly what the browser will. `el` is the one
	// name Datastar binds for a plain attribute (`data-init`).
	src := "const __datastarExpr = new Function('el', " + strconv.Quote(components.AutoScrollExpr) + ");\n"
	path := filepath.Join(t.TempDir(), "expr.js")
	if werr := os.WriteFile(path, []byte(src), 0o600); werr != nil {
		t.Fatalf("write expression: %v", werr)
	}
	// NOTE: the file is EXECUTED, not `node --check`ed. `new Function(...)`
	// is a runtime call, so a syntax check of the file passes even with a
	// broken body — proven with a negative control while writing this (the
	// old statement form parsed clean under --check and only failed when
	// run). Executing it is what makes the browser's compile failure the
	// test's failure.
	if out, cerr := exec.CommandContext(t.Context(), node, path).CombinedOutput(); cerr != nil {
		t.Fatalf("AutoScrollExpr does not compile as a Datastar expression body (%v):\n%s\nexpr: %s",
			cerr, out, components.AutoScrollExpr)
	}
}

// TestAutoScrollContract pins the anchor's shape: the expression rides
// data-init (re-run on every node an element morph inserts), the element is
// inert to assistive tech, and it is deliberately NOT hidden — a
// `display:none` element has no box and scrollIntoView on it is a no-op,
// which is the trap this assertion exists to keep closed.
func TestAutoScrollContract(t *testing.T) {
	t.Parallel()
	var buf bytes.Buffer
	if err := components.AutoScroll().Render(context.Background(), &buf); err != nil {
		t.Fatalf("AutoScroll: %v", err)
	}
	html := buf.String()
	for _, want := range []string{
		`data-init=`,
		`scrollIntoView`,
		`prefers-reduced-motion: reduce`,
		`aria-hidden="true"`,
	} {
		if !strings.Contains(html, want) {
			t.Errorf("AutoScroll missing %q in %q", want, html)
		}
	}
	// A bare `hidden` attribute renders as ` hidden` (leading space);
	// `aria-hidden="true"` must not be mistaken for it.
	if strings.Contains(html, " hidden") {
		t.Errorf("AutoScroll carries `hidden` — display:none leaves no box, so scrollIntoView is a no-op: %q", html)
	}
	if !strings.Contains(html, `aria-hidden="`+"true"+`"`) {
		t.Errorf("AutoScroll is not marked aria-hidden — an empty div needs the assistive-tech opt-out: %q", html)
	}
	if strings.Contains(html, `id="`) {
		t.Errorf("AutoScroll carries an id — one anchor per appended fragment would duplicate it: %q", html)
	}
}
