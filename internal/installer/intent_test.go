// SCOPE:plugin — tests for advise intent routing (which command runs next)
package installer

import (
	"encoding/json"
	"strings"
	"testing"
)

// TestDetectIntent pins the wording → route table. The false-positive rules are
// the same wordHit asymmetry presets use (a 4-letter English word never soaks
// up a longer keyword), so the cases below include the ones that used to bite.
func TestDetectIntent(t *testing.T) {
	cases := []struct {
		need string
		want string
	}{
		{"keep this project up to date", intentUpdate},
		{"update my gogogo base", intentUpdate},
		{"we are behind upstream", intentUpdate},
		{"my existing app needs a cms", intentPort},
		{"retrofit whiteboard into our codebase already", intentPort},
		{"restore the whiteboard unit I trimmed", intentAddUnit},
		{"add a new feature for reports", intentNewFeature},
		{"scaffold a greenfield app", intentScaffold},
		// Nothing named: silence is not an intent.
		{"quantum toaster firmware", intentNone},
		{"", intentNone},
	}
	for _, tc := range cases {
		if got := detectIntent(tc.need); got != tc.want {
			t.Errorf("detectIntent(%q) = %q, want %q", tc.need, got, tc.want)
		}
	}
}

// TestResolveIntentOrder pins precedence: what the caller said beats what the
// wording implies, which beats the default a non-checkout path forces.
func TestResolveIntentOrder(t *testing.T) {
	if got, src := resolveIntent("update my app", intentPort, treeCheckout); got != intentPort || src != "given" {
		t.Errorf("given intent lost: (%q,%q)", got, src)
	}
	if got, src := resolveIntent("update my app", "", treeCheckout); got != intentUpdate || src != "detected" {
		t.Errorf("detection lost: (%q,%q)", got, src)
	}
	if got, src := resolveIntent("", "", treeNotCheckout); got != intentPort || src != "default" {
		t.Errorf("foreign-tree default lost: (%q,%q)", got, src)
	}
	if got, src := resolveIntent("", "", treeCheckout); got != intentNone || src != "" {
		t.Errorf("silence must stay silent: (%q,%q)", got, src)
	}
}

// TestRoutesNeverNameARefusedCommand is the route's contract: add and new
// refuse a tree they did not scaffold, so a foreign-tree route must never print
// them. A route that fails on first paste is worse than no route.
func TestRoutesNeverNameARefusedCommand(t *testing.T) {
	for _, intent := range intentNames {
		for _, step := range routeFor(intent, treeNotCheckout, "/tmp/foreign") {
			for _, refused := range []string{"gogogo add ", "gogogo new feature ", "gogogo migrate "} {
				if strings.Contains(step.Run, refused) {
					t.Errorf("intent %q on a foreign tree names a command that refuses: %q", intent, step.Run)
				}
			}
		}
	}
	// And the porting routes must actually carry the export, or the reader has
	// no way to get the files.
	for _, intent := range []string{intentPort, intentAddUnit} {
		steps := routeFor(intent, treeNotCheckout, "/tmp/foreign")
		if len(steps) == 0 || !strings.Contains(steps[0].Run, "export") {
			t.Errorf("intent %q on a foreign tree must lead with export, got %+v", intent, steps)
		}
	}
	// The given dir is substituted, so the line is copy-pasteable.
	if got := routeFor(intentUpdate, treeCheckout, "/tmp/app")[0].Run; !strings.Contains(got, "/tmp/app") {
		t.Errorf("route did not substitute the directory: %q", got)
	}
}

// TestAdviseIntentOverridesTheWording checks the --intent/`intent` argument
// through the public entry point, including the JSON provenance fields.
func TestAdviseIntentOverridesTheWording(t *testing.T) {
	out, err := Advise("keep this project up to date", planFormatJSON, "", intentPort)
	if err != nil {
		t.Fatalf("Advise: %v", err)
	}
	var doc adviseDoc
	if err := json.Unmarshal([]byte(out), &doc); err != nil {
		t.Fatalf("unmarshal: %v", err)
	}
	if doc.Intent != intentPort || doc.IntentSource != "given" {
		t.Errorf("intent = (%q,%q), want (port, given)", doc.Intent, doc.IntentSource)
	}
	if len(doc.Next) == 0 {
		t.Error("a named intent must carry a route")
	}
}

func TestAdviseRejectsUnknownIntent(t *testing.T) {
	_, err := Advise("", planFormatText, "", "yolo")
	if err == nil {
		t.Fatal("Advise must reject an unknown intent instead of ignoring it")
	}
	for _, name := range intentNames {
		if !strings.Contains(err.Error(), name) {
			t.Errorf("error must list %q so the caller can retry: %v", name, err)
		}
	}
}

// TestCmsNeedRoutesToContentModel pins the lay-word coverage that was missing:
// "cms" matched no preset, so the answer fell back to the full 36-row table.
// The point is not the word list — it is that the answer then names the real
// mechanism (a slice you own via `new feature`), not a unit to install.
func TestCmsNeedRoutesToContentModel(t *testing.T) {
	doc := buildAdvise("i need a cms for part of my app")
	var found *advisePreset
	for i := range doc.Presets {
		if doc.Presets[i].Name == "content-model" {
			found = &doc.Presets[i]
		}
	}
	if found == nil {
		t.Fatalf("\"cms\" must match the content-model preset, got %+v", doc.Presets)
	}
	if !strings.Contains(found.Note, "new feature") {
		t.Errorf("the content-model note must name the real mechanism (new feature), got %q", found.Note)
	}
}

// TestRenderForeignTreePrintsTheCopySurface is the text contract for the fourth
// scope: the paths, the route, and no claim that anything installs.
func TestRenderForeignTreePrintsTheCopySurface(t *testing.T) {
	foreign := t.TempDir()
	writeFile(t, foreign+"/go.mod", "module example.com/x\n\ngo 1.22\n")

	out, err := Advise("realtime whiteboard", planFormatText, foreign, "")
	if err != nil {
		t.Fatalf("Advise: %v", err)
	}
	for _, want := range []string{
		"foreign tree",
		"next (intent: port, default)",
		"copy: features/whiteboard",
		"gogogo export",
		"Nothing installs here",
	} {
		if !strings.Contains(out, want) {
			t.Errorf("foreign-tree output missing %q:\n%s", want, out)
		}
	}
}
