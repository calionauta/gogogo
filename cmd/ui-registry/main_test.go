package main

import (
	"strings"
	"testing"

	"github.com/calionauta/gogogo/internal/uiregistry"
)

func fixture() *uiregistry.Registry {
	return &uiregistry.Registry{Version: 1, Entries: []uiregistry.Entry{
		{
			ID: "empty-state", Title: "Empty state", When: "a list is empty", Avoid: "ad-hoc margins",
			Source: "features/x/page.templ", Shows: []string{"ui-empty"}, Doc: "docs/ui-compositions.md#1-empty-state",
		},
		{
			ID: "account-row", Title: "Account row", When: "the signed-in identity", Avoid: "icon toggles",
			Source: "features/y/page.templ", Shows: []string{"app-rail-foot"}, Doc: "docs/ui-compositions.md#2-account-row",
		},
	}}
}

// list is the discovery surface: every entry, sorted by id so two runs agree.
func TestListIsSortedAndComplete(t *testing.T) {
	out := list(fixture())
	if strings.Index(out, "account-row") > strings.Index(out, "empty-state") {
		t.Fatalf("entries must be sorted by id, got:\n%s", out)
	}
	for _, want := range []string{"account-row", "empty-state", "features/x/page.templ", "2 compositions"} {
		if !strings.Contains(out, want) {
			t.Fatalf("list output missing %q:\n%s", want, out)
		}
	}
}

// show is the assembly surface: the rule of use, the anti-pattern, the source
// and the doc anchor are what an agent needs to copy the shape instead of
// inventing one.
func TestShowPrintsTheRuleOfUse(t *testing.T) {
	out, ok := show(fixture(), "empty-state", t.TempDir())
	if !ok {
		t.Fatal("show did not find the entry")
	}
	for _, want := range []string{
		"Empty state", "When:", "a list is empty", "Avoid:", "ad-hoc margins",
		"Source: features/x/page.templ", "docs/ui-compositions.md#1-empty-state", "ui-empty",
	} {
		if !strings.Contains(out, want) {
			t.Fatalf("show output missing %q:\n%s", want, out)
		}
	}
	if _, ok := show(fixture(), "nope", t.TempDir()); ok {
		t.Fatal("show invented an entry")
	}
}
