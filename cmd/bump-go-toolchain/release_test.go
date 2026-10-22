// SCOPE:core — the release feed and the release-existence check.
package main

import (
	"io"
	"net/http"
	"net/http/httptest"
	"testing"
)

// fakeOnline is the network edge pointed at a server that says the given
// releases exist. The real edge is go.dev; a test that reached it would be
// slow, offline-hostile, and would fail the day the next patch ships.
func fakeOnline(t *testing.T, feed string, published ...string) *online {
	t.Helper()

	mux := http.NewServeMux()
	if feed != "" {
		mux.HandleFunc("/feed.json", func(w http.ResponseWriter, _ *http.Request) {
			_, _ = io.WriteString(w, feed)
		})
	}
	for _, v := range published {
		// A version that is NOT in this list falls through to the mux's 404,
		// which is exactly what go.dev does for a release that does not exist.
		mux.HandleFunc("/dl/go"+v+".src.tar.gz", func(w http.ResponseWriter, r *http.Request) {
			if r.Method != http.MethodHead {
				t.Errorf("the release check used %s, want HEAD — downloading a tarball "+
					"to ask whether it exists is not a check", r.Method)
			}
			w.WriteHeader(http.StatusOK)
		})
	}

	srv := httptest.NewServer(mux)
	t.Cleanup(srv.Close)
	return &online{
		client:  srv.Client(),
		feed:    srv.URL + "/feed.json",
		release: srv.URL + "/dl/go%s.src.tar.gz",
	}
}

// TestParseLatestPatch picks the newest patch of the CURRENT minor line out
// of the release feed — and ignores the newer minor line, which is a human
// decision.
func TestParseLatestPatch(t *testing.T) {
	t.Parallel()

	feed := `[
	 {"version":"go1.28.0","stable":true},
	 {"version":"go1.27.3","stable":true},
	 {"version":"go1.27.10","stable":true},
	 {"version":"go1.26.15","stable":true},
	 {"version":"go1.27.4","stable":true}
	]`

	got, err := parseLatestPatch([]byte(feed), "1.27")
	if err != nil {
		t.Fatalf("parseLatestPatch: %v", err)
	}
	if got != "1.27.10" {
		t.Errorf("parseLatestPatch = %q, want 1.27.10 (newest patch of the 1.27 line)", got)
	}

	got, err = parseLatestPatch([]byte(feed), "1.26")
	if err != nil {
		t.Fatalf("parseLatestPatch: %v", err)
	}
	if got != "1.26.15" {
		t.Errorf("parseLatestPatch = %q, want 1.26.15", got)
	}

	got, err = parseLatestPatch([]byte(feed), "1.25")
	if err != nil {
		t.Fatalf("parseLatestPatch: %v", err)
	}
	if got != "" {
		t.Errorf("parseLatestPatch = %q for a line the feed does not carry, want \"\"", got)
	}
}
