// The version badge's live sync contract.
package ui

import (
	"context"
	"strings"
	"testing"
)

// TestBadgeSyncReadsHealthHeader pins the dynamic version badge: the sync must
// read the X-Gogogo-Build HEADER of /health, where healthHandler puts the live
// build tag. Reading the body instead pins the badge to the SSR label forever,
// because the body is always "ok" (the staleness shape PR #378 left behind:
// the badge never updates, it only looks dynamic).
func TestBadgeSyncReadsHealthHeader(t *testing.T) {
	html := renderShell(context.Background(), t)
	for _, want := range []string{
		`r.headers.get("X-Gogogo-Build")`,
		`fetch("/health", { cache: "no-store" })`,
	} {
		if !strings.Contains(html, want) {
			t.Fatalf("badge sync missing %q", want)
		}
	}
	if strings.Contains(html, "return r.text();") {
		t.Fatal("badge sync must not read the /health body (it is always 'ok'); the live tag is the X-Gogogo-Build header")
	}
}
