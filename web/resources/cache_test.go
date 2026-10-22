// SCOPE:core — the cache contract for embedded assets.
//
// These assertions exist because the contract was documented in AssetHandler
// and never enforced: an unversioned URL revalidated on every navigation, and
// nothing checked that the versioned one actually bought immutability. With a
// CDN in front, `must-revalidate` means the edge answers REVALIDATED instead of
// HIT — every page load crosses the tunnel once per asset.
package resources

import (
	"context"
	"io/fs"
	"net/http"
	"net/http/httptest"
	"net/url"
	"strings"
	"testing"
)

const (
	immutablePolicy  = "public, max-age=31536000, immutable"
	revalidatePolicy = "public, max-age=0, must-revalidate"
	appCSS           = "app.css"
	appCSSPath       = "/static/app.css"
)

func get(t *testing.T, h http.Handler, target string, hdr map[string]string) *httptest.ResponseRecorder {
	t.Helper()
	req := httptest.NewRequestWithContext(context.Background(), http.MethodGet, target, nil)
	for k, v := range hdr {
		req.Header.Set(k, v)
	}
	rec := httptest.NewRecorder()
	h.ServeHTTP(rec, req)
	return rec
}

func TestAssetHandlerCachePolicy(t *testing.T) {
	t.Parallel()
	h := AssetHandler(StaticFS(), "/static")

	versioned := StaticURL(appCSS)
	u, err := url.Parse(versioned)
	if err != nil {
		t.Fatalf("parse StaticURL: %v", err)
	}
	fp := u.Query().Get("v")
	if fp == "" {
		t.Fatalf("StaticURL(%q) = %q, want a ?v= fingerprint", appCSS, versioned)
	}

	for _, tc := range []struct {
		name   string
		target string
		want   string
	}{
		{"versioned url is immutable", versioned, immutablePolicy},
		{"bare url revalidates", appCSSPath, revalidatePolicy},
		{"wrong fingerprint revalidates", appCSSPath + "?v=deadbeef", revalidatePolicy},
		{"empty fingerprint revalidates", appCSSPath + "?v=", revalidatePolicy},
	} {
		t.Run(tc.name, func(t *testing.T) {
			t.Parallel()
			rec := get(t, h, tc.target, nil)
			if rec.Code != http.StatusOK {
				t.Fatalf("GET %s = %d, want 200", tc.target, rec.Code)
			}
			if got := rec.Header().Get("Cache-Control"); got != tc.want {
				t.Fatalf("GET %s Cache-Control = %q, want %q", tc.target, got, tc.want)
			}
		})
	}
}

// TestAssetHandlerRevalidatesBothPolicies pins that the 304 path survives the
// immutable branch: a client that already holds the bytes must never be handed
// them again, whichever URL shape it used.
func TestAssetHandlerRevalidatesBothPolicies(t *testing.T) {
	t.Parallel()
	h := AssetHandler(StaticFS(), "/static")

	for _, target := range []string{appCSSPath, StaticURL(appCSS)} {
		first := get(t, h, target, nil)
		etag := first.Header().Get("ETag")
		if etag == "" {
			t.Fatalf("GET %s: no ETag", target)
		}
		second := get(t, h, target, map[string]string{"If-None-Match": etag})
		if second.Code != http.StatusNotModified {
			t.Fatalf("GET %s with matching If-None-Match = %d, want 304", target, second.Code)
		}
		if second.Body.Len() != 0 {
			t.Fatalf("GET %s 304 carried %d bytes, want none", target, second.Body.Len())
		}
	}
}

// TestStaticURLEveryAssetRoundTrips walks the embedded tree so a NEW asset
// cannot ship with a helper that silently degrades to the revalidating path
// (StaticURL returns the bare path for unknown names by design).
func TestStaticURLEveryAssetRoundTrips(t *testing.T) {
	t.Parallel()
	h := AssetHandler(StaticFS(), "/static")
	fsys := StaticFS()

	err := fs.WalkDir(fsys, ".", func(p string, d fs.DirEntry, err error) error {
		if err != nil || d.IsDir() {
			return err
		}
		target := StaticURL(p)
		if !strings.Contains(target, "?v=") {
			t.Errorf("StaticURL(%q) = %q, want a fingerprinted URL", p, target)
			return nil
		}
		rec := get(t, h, target, nil)
		if rec.Code != http.StatusOK {
			t.Errorf("GET %s = %d, want 200", target, rec.Code)
			return nil
		}
		if got := rec.Header().Get("Cache-Control"); got != immutablePolicy {
			t.Errorf("GET %s Cache-Control = %q, want %q", target, got, immutablePolicy)
		}
		return nil
	})
	if err != nil {
		t.Fatalf("walk static fs: %v", err)
	}
}
