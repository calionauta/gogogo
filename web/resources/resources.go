// SCOPE:core - DO NOT REMOVE - Embedded static assets (CSS, JS, etc.).
package resources

import (
	"crypto/sha256"
	"embed"
	"encoding/hex"
	"io/fs"
	"mime"
	"net/http"
	"path"
	"strconv"
	"strings"
	"sync"
)

//go:embed static/*
var staticEmbed embed.FS

// StaticFS returns an fs.FS for serving embedded static files (the
// contents of the static/ directory). Used both for route discovery and as
// the source for AssetHandler.
func StaticFS() fs.FS {
	sub, err := fs.Sub(staticEmbed, "static")
	if err != nil {
		panic("resources: missing static directory: " + err.Error())
	}
	return sub
}

// assetFingerprintLen is how many hex characters of the content hash go into
// ?v=. 16 (64 bits) is far past collision risk for a build artifact set this
// size and keeps the query string short.
const assetFingerprintLen = 16

var (
	staticURLOnce sync.Once
	staticURLs    map[string]string
)

// StaticURL returns the cache-busting URL for one embedded asset:
// "/static/app.min.css?v=<content hash>".
//
// Templates emit assets through this instead of hardcoding the path, because
// the URL is what lets the browser and any CDN in front of the app cache the
// file immutably: the query names the bytes, so a deploy that changes the file
// produces a different URL and staleness is impossible (see AssetHandler).
//
// An unknown name returns the plain path. That still serves, it just falls
// back to per-navigation revalidation — a missing entry is a cheap mistake,
// never a broken page.
func StaticURL(name string) string {
	staticURLOnce.Do(func() {
		staticURLs = map[string]string{}
		fsys := StaticFS()
		if err := fs.WalkDir(fsys, ".", func(p string, d fs.DirEntry, err error) error {
			if err != nil || d.IsDir() {
				return err
			}
			data, rerr := fs.ReadFile(fsys, p)
			if rerr != nil {
				return rerr
			}
			sum := sha256.Sum256(data)
			staticURLs[p] = "/static/" + p + "?v=" + hex.EncodeToString(sum[:])[:assetFingerprintLen]
			return nil
		}); err != nil {
			panic("resources: build static URL table: " + err.Error())
		}
	})
	if u, ok := staticURLs[strings.TrimPrefix(name, "/")]; ok {
		return u
	}
	return "/static/" + strings.TrimPrefix(name, "/")
}

// URLFor returns the cache-busting URL for an asset that lives in a feature's
// OWN embedded bundle, served under a different URL prefix
// (features/notes serves at /static/notes, features/whiteboard at /static/wb).
//
// The core bundle and the feature bundles are the same problem: the URL must
// name the bytes for the browser and any CDN to hold the file immutably. The
// feature owns both the file and the path it answers on (its trim takes the
// mount with it), so the feature owns the fingerprint too — it hashes its own
// embedded file.
//
// An unknown name returns the plain path, exactly like StaticURL: a missing
// entry degrades to revalidation, it never breaks the page.
func URLFor(fsys fs.FS, prefix, name string) string {
	sum, err := hashAsset(fsys, name)
	if err != nil {
		return prefix + "/" + strings.TrimPrefix(name, "/")
	}
	return prefix + "/" + strings.TrimPrefix(name, "/") + "?v=" + sum
}

// hashAsset returns the fingerprint of one file in fsys: the first
// assetFingerprintLen hex characters of its SHA-256.
func hashAsset(fsys fs.FS, name string) (string, error) {
	data, err := fs.ReadFile(fsys, strings.TrimPrefix(name, "/"))
	if err != nil {
		return "", err
	}
	sum := sha256.Sum256(data)
	return hex.EncodeToString(sum[:])[:assetFingerprintLen], nil
}

// asset is one embedded file, pre-read at startup together with its
// content hash and MIME type.
type asset struct {
	data  []byte
	etag  string // quoted sha256 hex, a strong validator for revalidation
	fp    string // bare sha256 prefix, the ?v= value that fingerprints the URL
	ctype string
}

// AssetHandler serves every file in fsys at URLs under urlPrefix (e.g.
// "/static" maps /static/foo.js to the fsys entry "foo.js"). It is the
// single static-serving path for the app's embedded assets and implements
// the cache contract that prevents stale content after a deploy:
//
//	Cache-Control: public, max-age=0, must-revalidate
//	ETag: <sha256 of content>
//
// Because embed.FS carries no mtime, Go's http.FileServer ETag would be
// size-only and collide when a file's bytes change without its size changing
// — this handler hashes the content instead, so a changed asset always yields
// a different ETag and a stale 304 is impossible.
//
// A request that carries the asset's own fingerprint (`?v=<fp>`, see
// StaticURL) is answered with the long-lived, immutable policy instead. That
// is safe by construction rather than by trust: the URL names the bytes, so a
// changed asset is a DIFFERENT URL and can never be served from a stale cache.
// Revalidation is the fallback for unversioned URLs (old bookmarks, the
// service-worker precache list), not the default.
//
// The distinction is not cosmetic. `must-revalidate` forces every navigation
// to spend a round trip per asset, and a CDN in front of the app then serves
// REVALIDATED instead of HIT — with the origin reachable only over a tunnel,
// that is the difference between an edge hit and crossing the tunnel for a
// 348 KB stylesheet.
func AssetHandler(fsys fs.FS, urlPrefix string) http.Handler {
	assets := map[string]asset{}
	if err := fs.WalkDir(fsys, ".", func(p string, d fs.DirEntry, err error) error {
		if err != nil {
			return err
		}
		if d.IsDir() {
			return nil
		}
		data, err := fs.ReadFile(fsys, p)
		if err != nil {
			return err
		}
		sum := sha256.Sum256(data)
		hexSum := hex.EncodeToString(sum[:])
		assets[p] = asset{
			data:  data,
			etag:  `"` + hexSum + `"`,
			fp:    hexSum[:assetFingerprintLen],
			ctype: mime.TypeByExtension(strings.ToLower(path.Ext(p))),
		}
		return nil
	}); err != nil {
		panic("resources: build asset table: " + err.Error())
	}

	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		name := strings.TrimPrefix(r.URL.Path, urlPrefix)
		name = strings.TrimPrefix(name, "/")
		a, ok := assets[name]
		if !ok {
			http.NotFound(w, r)
			return
		}
		if r.URL.Query().Get("v") == a.fp {
			w.Header().Set("Cache-Control", "public, max-age=31536000, immutable")
		} else {
			w.Header().Set("Cache-Control", "public, max-age=0, must-revalidate")
		}
		w.Header().Set("ETag", a.etag)
		// Strong revalidation: a matching If-None-Match means the cached
		// copy is still the current content — answer 304 with no body.
		if inm := r.Header.Get("If-None-Match"); inm != "" && (inm == a.etag || inm == "*") {
			w.WriteHeader(http.StatusNotModified)
			return
		}
		if a.ctype != "" {
			w.Header().Set("Content-Type", a.ctype)
		} else {
			w.Header().Set("Content-Type", "application/octet-stream")
		}
		w.Header().Set("Content-Length", strconv.Itoa(len(a.data)))
		if r.Method == http.MethodHead {
			return
		}
		// A client that goes away mid-write is normal HTTP; there is nothing
		// actionable to do with the error at this layer.
		_, _ = w.Write(a.data)
	})
}
