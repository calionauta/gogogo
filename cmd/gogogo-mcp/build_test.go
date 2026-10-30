package main

import (
	"context"
	"strings"
	"testing"
)

// TestBuildPrerequisites pins both halves and the platform switch: a tool that
// silently returned one platform would still pass a "no error" check.
func TestBuildPrerequisites(t *testing.T) {
	all, _, err := handleBuildPrerequisites(context.Background(), nil, buildArgs{})
	if err != nil {
		t.Fatal(err)
	}
	text := toolText(t, all)
	for _, want := range []string{"make desktop", "make mobile", "cmd/mobile", "ncruces"} {
		if !strings.Contains(text, want) {
			t.Errorf("both-platform guidance missing %q", want)
		}
	}
	mobile, _, err := handleBuildPrerequisites(context.Background(), nil, buildArgs{Platform: "mobile"})
	if err != nil {
		t.Fatal(err)
	}
	mt := toolText(t, mobile)
	if !strings.Contains(mt, "make mobile") || strings.Contains(mt, "make desktop") {
		t.Errorf("mobile filter wrong: %s", mt)
	}
}
