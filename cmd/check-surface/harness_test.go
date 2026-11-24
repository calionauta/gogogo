package main

import (
	"strings"
	"testing"
)

// A whole dead file survived three gates because a `cmd/desktop` demo called
// it. `deadcode` was RIGHT to keep it — the demo is a real caller and RTA saw
// the edge — and `-methods` was right to keep it too, because it is genuinely
// selected. No gate was wrong; the answer was just not the one anybody needed.
//
// These pin the measurement that makes that shape visible, and the guard that
// stops it from shipping as a gate: harness-only is REPORTED in -depth and
// never in the exit code, because a command-line shell calling a package is
// legitimate. The failing part is a file nobody renders.

// TestHarnessOnlyIsMeasured builds the shape that bit us: an exported method
// whose only caller is a cmd/ main package.
func TestHarnessOnlyIsMeasured(t *testing.T) {
	root := writeModule(t, map[string]string{
		"lib/lib.go": `package lib

type Session struct{}

func (Session) Cursor() int { return 1 }
`,
		"cmd/harness/main.go": `package main

import "github.com/calionauta/gogogo/fixture/lib"

func main() { _ = lib.Session{}.Cursor() }
`,
	})
	// It is NOT dead: the harness calls it, and reporting it dead would be a
	// false positive that teaches people to ignore the gate.
	for _, g := range gapNames(t, root) {
		if strings.Contains(g, "Cursor") {
			t.Errorf("Cursor reported as unreachable (%q); a harness caller is a caller", g)
		}
	}

	findings, err := scan(root)
	if err != nil {
		t.Fatalf("scan() = %v", err)
	}
	found := false
	for i := range findings {
		if findings[i].cmdOnly() {
			found = true
		}
	}
	if !found {
		t.Error("no finding reported as harness-only; the demo-caller shape is invisible again")
	}
}

// TestAHarnessAloneIsNotAReasonToKeepAFile is the rule stated as a test: the
// measurement must count the export as USED (so it never becomes a false
// positive) and the depth report must name it apart. Two facts, one shape.
func TestAHarnessAloneIsNotAReasonToKeepAFile(t *testing.T) {
	root := writeModule(t, map[string]string{
		"lib/lib.go": `package lib

func Rendered() int { return 1 }
`,
		"cmd/harness/main.go": `package main

import "github.com/calionauta/gogogo/fixture/lib"

func main() { _ = lib.Rendered() }
`,
	})
	findings, err := scan(root)
	if err != nil {
		t.Fatalf("scan() = %v", err)
	}
	var target *finding
	for i := range findings {
		if strings.HasSuffix(findings[i].name, ".Rendered") {
			target = &findings[i]
			break
		}
	}
	if target == nil {
		t.Fatal("Rendered not in the measurement at all")
	}
	if target.class == classDead {
		t.Fatalf("class = dead; a cmd/ caller is a caller (deadcode agrees)")
	}
	if !target.cmdOnly() {
		t.Errorf("cmdOnly() = false for an export whose only caller is cmd/ (extUses=%d cmdUses=%d)",
			target.extUses, target.cmdUses)
	}
}
