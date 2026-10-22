// SCOPE:plugin — tests for the migration file-impact report (`migrate --files`)
package installer

import (
	"context"
	"fmt"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
)

// TestOwnerOfPathPrefersTheMostSpecificOwner pins the matching rule against the
// real registry: the report names the CAPABILITY a file serves (`notes`), not
// the unit that carries it (`whiteboard`), because that is the answer to "what
// is this file?" — and the unit stays available for the apply-or-skip verdict.
func TestOwnerOfPathPrefersTheMostSpecificOwner(t *testing.T) {
	index := ownedPathIndex()
	cases := map[string]string{
		"features/whiteboard/handlers/chi.go": "whiteboard",
		"features/whiteboard/embed.go":        "whiteboard",
		// notes.js serves notes, which whiteboard trims with it — the label
		// must say notes, not whiteboard.
		"features/notes/static/notes.js": "notes",
		// internal/collab belongs to collab, and nothing broader may claim it
		// first.
		"internal/collab/docstore.go": "collab",
		// server is a capability no unit owns: it ships in every tree, so its
		// files are core — the label says which surface, the verdict says core.
		"internal/server/chi.go":           "server",
		"web/resources/static/app.min.css": "",
		"README.md":                        "",
	}
	for path, want := range cases {
		if got := ownerOfPath(index, path); got != want {
			t.Errorf("ownerOfPath(%q) = %q, want %q", path, got, want)
		}
	}
}

// TestClassifyImpactSeparatesKeptFromRemoved is the report's verdict: a file in
// a unit this tree trimmed is work NOT to do, and it must never be mixed in
// with the files that are.
func TestClassifyImpactSeparatesKeptFromRemoved(t *testing.T) {
	got := classifyImpact([]impactFile{
		{Path: "internal/dagnats/boot.go", Status: "modified", Additions: 4},
		{Path: "internal/server/chi.go", Status: "modified", Additions: 2},
		{Path: "features/whiteboard/handlers/chi.go", Status: "added", Additions: 9},
	}, map[string]bool{"dagnats": true})

	byPath := map[string]impactFile{}
	for _, f := range got {
		byPath[f.Path] = f
	}
	if f := byPath["internal/dagnats/boot.go"]; f.Unit != "dagnats" || f.UnitState != impactRemoved {
		t.Errorf("dagnats file = %+v, want unit dagnats / state removed", f)
	}
	if f := byPath["internal/server/chi.go"]; f.Unit != "" || f.UnitState != impactCore {
		t.Errorf("core file = %+v, want empty unit / state core", f)
	}
	if f := byPath["features/whiteboard/handlers/chi.go"]; f.Capability != "whiteboard" ||
		f.Unit != "whiteboard" || f.UnitState != impactKept {
		t.Errorf("kept file = %+v, want capability+unit whiteboard / state kept", f)
	}
	// Sorting is by verdict then path, so the skippable work lands last.
	if got[len(got)-1].UnitState != impactRemoved {
		t.Errorf("removed files must sort last, got %q", got[len(got)-1].UnitState)
	}
}

func TestImpactLineCounts(t *testing.T) {
	line := impactLine([]impactFile{
		{UnitState: impactCore},
		{UnitState: impactCore},
		{UnitState: impactKept},
		{UnitState: impactRemoved},
		{UnitState: impactRemoved},
		{UnitState: impactRemoved},
	})
	want := "summary: 2 core, 1 in units you kept, 3 in units you removed (skippable)"
	if line != want {
		t.Errorf("impactLine = %q, want %q", line, want)
	}
}

func TestFetchCompareParsesTheList(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if !strings.Contains(r.URL.Path, "/repos/acme/tpl/compare/v1...v2") {
			t.Errorf("unexpected path %q", r.URL.Path)
		}
		if r.Header.Get("User-Agent") == "" {
			t.Error("request has no User-Agent: GitHub rejects those")
		}
		fmt.Fprint(w, `{"files":[{"filename":"a.go","status":"modified","additions":3,"deletions":1},`+
			`{"filename":"b/doc.md","status":"added","additions":5,"deletions":0}]}`)
	}))
	defer srv.Close()

	files, truncated, err := fetchCompare(context.Background(), srv.URL, "acme/tpl", "v1", "v2")
	if err != nil {
		t.Fatalf("fetchCompare: %v", err)
	}
	if truncated {
		t.Error("two files must not be reported as truncated")
	}
	if len(files) != 2 || files[0].Path != "a.go" || files[1].Additions != 5 {
		t.Errorf("parsed %+v", files)
	}
}

// TestFetchCompareFlagsTruncation keeps the report from presenting a capped
// list as the whole range.
func TestFetchCompareFlagsTruncation(t *testing.T) {
	var sb strings.Builder
	sb.WriteString(`{"files":[`)
	for i := range compareFileLimit {
		if i > 0 {
			sb.WriteString(",")
		}
		fmt.Fprintf(&sb, `{"filename":"f%03d.go","status":"modified"}`, i)
	}
	sb.WriteString("]}")
	body := sb.String()

	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		fmt.Fprint(w, body)
	}))
	defer srv.Close()

	if _, truncated, err := fetchCompare(context.Background(), srv.URL, "acme/tpl", "v1", "v2"); err != nil || !truncated {
		t.Errorf("truncated = %v (err %v), want true", truncated, err)
	}
}

// TestFetchCompareReportsHTTPStatus keeps a typo'd ref and a spent rate limit
// distinguishable: the status is the message.
func TestFetchCompareReportsHTTPStatus(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		http.Error(w, `{"message":"Not Found"}`, http.StatusNotFound)
	}))
	defer srv.Close()

	_, _, err := fetchCompare(context.Background(), srv.URL, "acme/tpl", "v1", "v404")
	if err == nil || !strings.Contains(err.Error(), "404") {
		t.Errorf("err = %v, want it to name the 404", err)
	}
}

// TestOwnerOfPathTakesTheLongestMatch pins the rule that makes a nested
// capability safe. It is deliberately built on a HAND-MADE, unordered index:
// the rule must not depend on registry order, or it holds only until someone
// adds a capability inside another one's directory.
func TestOwnerOfPathTakesTheLongestMatch(t *testing.T) {
	index := []ownedPath{
		{capability: "broad", path: "internal"},
		{capability: "narrow", path: "internal/collab"},
		{capability: "file", path: "README.md", exact: true},
	}
	cases := map[string]string{
		"internal/collab/docstore.go": "narrow",
		"internal/server/chi.go":      "broad",
		"README.md":                   "file",
		// An exact entry names ONE file; it must never act as a prefix.
		"README.md.bak":   "",
		"cmd/web/main.go": "",
	}
	for path, want := range cases {
		if got := ownerOfPath(index, path); got != want {
			t.Errorf("ownerOfPath(%q) = %q, want %q", path, got, want)
		}
	}
}
