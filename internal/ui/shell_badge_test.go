// The version badge's live sync contract.
package ui

import (
	"bytes"
	"context"
	"strings"
	"testing"

	"github.com/a-h/templ"
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

// TestBuildBadgeCountsExactlyOne pins the dedupe: the rail brand carries the
// only version badge. A page must not hang a second one (the /config header
// duplicated it for years).
func TestBuildBadgeCountsExactlyOne(t *testing.T) {
	ctx := WithNav(context.Background(), nil)
	var buf bytes.Buffer
	d := Data{Title: "Test", Module: "todo", Role: "admin", BuildLabel: "v9.9.9", BuildCommit: "abc1234"}
	if err := Shell(d, templ.NopComponent, templ.NopComponent, templ.NopComponent, templ.NopComponent).
		Render(ctx, &buf); err != nil {
		t.Fatalf("render shell: %v", err)
	}
	if got := strings.Count(buf.String(), `id="gogogo-build-badge"`); got != 1 {
		t.Fatalf("gogogo-build-badge rendered %d times, want exactly 1", got)
	}
}
