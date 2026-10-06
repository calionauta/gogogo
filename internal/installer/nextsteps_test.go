// SCOPE:layer=infra,removal=plugin — installer engine: scaffold next-steps
package installer

import (
	"strings"
	"testing"
)

// TestNextStepsOmitsTrimmedConsole covers a UX bug in the dagnats trim.
//
// buildNextSteps hard-coded "http://localhost:8080/dagnats/", so a scaffold
// whose dagnats unit had just been deleted still told the user to visit the
// DagNats console — a route that no longer exists in that checkout. The
// drop list now decides, and the line is omitted rather than printed broken.
func TestNextStepsOmitsTrimmedConsole(t *testing.T) {
	t.Parallel()

	withConsole := buildNextSteps("/tmp/x", nil)
	if !strings.Contains(withConsole.Flows, "/dagnats/") {
		t.Fatalf("with dagnats kept, Flows should advertise the console; got %q", withConsole.Flows)
	}

	dropped := []trimUnit{{id: unitDagnats}}
	withoutConsole := buildNextSteps("/tmp/x", dropped)
	if withoutConsole.Flows != "" {
		t.Errorf("with dagnats trimmed, Flows must be empty; got %q", withoutConsole.Flows)
	}

	// The printed form must actually omit the line, not print an empty one.
	var b strings.Builder
	printNextSteps(&b, "/tmp/x", dropped)
	out := b.String()
	if strings.Contains(out, "workflows:") {
		t.Errorf("trimmed scaffold still prints a workflows line:\n%s", out)
	}
	if !strings.Contains(out, "app:") || !strings.Contains(out, "login:") {
		t.Errorf("trimming dagnats must not remove the other destinations:\n%s", out)
	}
}

// TestNextStepsPrintsConsoleWhenKept is the other half: the line must still
// appear on a scaffold that keeps dagnats, or the fix would silently remove
// a working hint.
func TestNextStepsPrintsConsoleWhenKept(t *testing.T) {
	t.Parallel()

	var b strings.Builder
	printNextSteps(&b, "/tmp/x", []trimUnit{{id: unitDagnats}})
	trimmed := b.String()
	if strings.Contains(trimmed, "/dagnats/") {
		t.Fatalf("trimmed output advertises /dagnats/:\n%s", trimmed)
	}

	b.Reset()
	printNextSteps(&b, "/tmp/x", nil)
	if !strings.Contains(b.String(), "/dagnats/") {
		t.Fatalf("kept output should advertise /dagnats/:\n%s", b.String())
	}
}
