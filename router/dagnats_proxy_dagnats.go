// SCOPE:feature - REMOVE if not using DagNats dashboard.
package router

import (
	"bytes"
	"io"
	"log"
	"net/http"
	"net/http/httputil"
	"net/url"
	"regexp"
	"strings"

	"github.com/pocketbase/pocketbase/core"

	"github.com/calionauta/gogogo/internal/routeutil"
)

// mountDagNatsDashboard reverse-proxies the DagNats engine's own HTTP
// console (it listens on cfg.DagNats.HTTPAddr, e.g. 127.0.0.1:8090) under
// the app's own origin at /dagnats/.
//
// Why a proxy instead of linking straight to :8090: the app is reached
// through a Cloudflare Tunnel that only forwards 443 -> the app port.
// Port 8090 is NOT tunneled, so a direct link to
// https://<deploy-host>:8090/ hangs forever (the user-observed
// "infinite loading"). Proxying through the already-tunneled app means
// the DagNats dashboard is reachable with zero extra infra/tunnel config.
//
// The DagNats console is a SPA that emits absolute paths (/console/,
// /ui/, /v1/, /docs, /openapi.json, /hooks/, /metrics, ...). We strip the
// /dagnats prefix on the way in and rewrite those absolute prefixes back
// to /dagnats... on the way out (HTML/JS/CSS bodies + Location headers),
// which is the standard "mount a sub-app under a subpath" technique.
func mountDagNatsDashboard(se *core.ServeEvent, upstream string) {
	// upstream is host:port (e.g. 127.0.0.1:8090) from config; ensure a
	// scheme for url.Parse.
	if !strings.HasPrefix(upstream, "http://") && !strings.HasPrefix(upstream, "https://") {
		upstream = "http://" + upstream
	}
	target, err := url.Parse(upstream)
	if err != nil {
		log.Printf("dagnats proxy: bad upstream %q: %v", upstream, err)
		return
	}
	proxy := &httputil.ReverseProxy{
		Rewrite: func(r *httputil.ProxyRequest) {
			r.SetURL(target)
			// Strip the /dagnats prefix so upstream sees e.g. /console/.
			r.Out.URL.Path = strings.TrimPrefix(r.Out.URL.Path, "/dagnats")
			if r.Out.URL.Path == "" {
				r.Out.URL.Path = "/"
			}
		},
		ModifyResponse: rewriteDagNatsPaths,
	}

	// Register each method explicitly — NOT Router.Any().
	//
	// The DagNats console is a full CRUD app: creating a trigger, editing a
	// workflow, cancelling a run and deleting a schedule all issue
	// POST/PUT/PATCH/DELETE, while the pages and assets are GET. The upstream
	// DagNats mux registers its handlers with `mux.Handle` (method-agnostic), so
	// a GET-only proxy in front of it turned every write into a 404 — exactly
	// the `create failed: 404 The requested resource wasn't found.` seen in the
	// UI. That was the bug this fixes.
	//
	// But Router.Any() is NOT the fix. It registers the route with an empty
	// method, which becomes a method-less pattern in Go 1.22+ ServeMux, and
	// such a pattern conflicts with the app's own `GET /`:
	//
	//   pattern "GET /" conflicts with pattern "/dagnats/{path...}":
	//   GET / matches fewer methods than /dagnats/{path...}, but has a more
	//   general path pattern
	//
	// That panic happens at route-registration time, so the binary crashes on
	// startup and the container restart-loops. Enumerating methods keeps every
	// pattern method-scoped, so `GET /` and `GET /dagnats/{path...}` are simply
	// two GET patterns and the more specific one wins — no conflict.
	handler := func(c *core.RequestEvent) error {
		// Buffer the body before handing it to the proxy. PocketBase wraps the
		// request body in its RereadableReadCloser, whose Read() rewinds itself
		// at EOF to allow multiple reads. httputil.ReverseProxy streams the body
		// to the Transport, which reads until EOF — so it sees the payload,
		// hits the automatic rewind, and reads it a SECOND time. The Transport
		// then writes twice ContentLength bytes and aborts with
		// "ContentLength=N with Body length 2N", surfacing as a 502 on every
		// POST/PUT/PATCH. (Verified: a GET passes, a POST with a 13-byte body
		// fails with ContentLength=13, Body length=26.) Replacing the body with
		// a plain bytes.Reader detaches it from that wrapper, so the proxy sees
		// exactly the bytes the client sent.
		//
		// Buffering is bounded to maxProxyBody: the console API carries small
		// JSON payloads (trigger config, workflow definition). A body larger
		// than the cap is rejected rather than silently truncated.
		if c.Request.Body != nil {
			body, err := io.ReadAll(io.LimitReader(c.Request.Body, maxProxyBody+1))
			if err != nil {
				return c.String(http.StatusBadRequest, "dagnats proxy: unreadable body")
			}
			if int64(len(body)) > maxProxyBody {
				return c.String(http.StatusRequestEntityTooLarge, "dagnats proxy: body too large")
			}
			c.Request.Body = io.NopCloser(bytes.NewReader(body))
			c.Request.ContentLength = int64(len(body))
		}
		proxy.ServeHTTP(c.Response, c.Request)
		return nil
	}

	// Register per method, never with Router.Any() — see
	// internal/routeutil for why Any() panics at startup here.
	routeutil.RegisterAll(se.Router,
		[]string{"/dagnats", "/dagnats/{path...}"},
		handler,
	)
}

// maxProxyBody caps how much of a request body the DagNats proxy will buffer.
// The console API sends small JSON documents; 8 MiB is far above any real
// payload while keeping a malicious or runaway client from pinning memory.
const maxProxyBody = 8 << 20

// dagNatsAbsPrefixes are the absolute paths the DagNats SPA emits that
// must be re-prefixed with /dagnats so they resolve through the proxy.
var dagNatsAbsPrefixes = []string{
	"/console", "/ui", "/v1", "/docs", "/openapi.json",
	"/hooks", "/metrics", "/health", "/ready", "/debug",
}

// dagNatsPathRe matches an absolute DagNats path at the start of an
// attribute/URL (e.g. href="/console/...", fetch("/v1/..."),
// src="/ui/..."). It captures the leading quote/brace so we can preserve
// it and only rewrite the path.
var dagNatsPathRe = func() *regexp.Regexp {
	alts := strings.Join(dagNatsAbsPrefixes, "|")
	return regexp.MustCompile(`(["'(= ])(` + alts + `)(/|$)`)
}()

// rewriteDagNatsPaths rewrites absolute DagNats paths in the response body
// (HTML/JS/CSS) and the Location header to the /dagnats subpath.
func rewriteDagNatsPaths(resp *http.Response) error {
	loc := resp.Header.Get("Location")
	if loc != "" {
		for _, p := range dagNatsAbsPrefixes {
			if strings.HasPrefix(loc, p) {
				resp.Header.Set("Location", "/dagnats"+loc)
				break
			}
		}
	}
	ct := resp.Header.Get("Content-Type")
	if !strings.Contains(ct, "text/html") &&
		!strings.Contains(ct, "javascript") &&
		!strings.Contains(ct, "text/css") {
		return nil
	}
	body, err := io.ReadAll(resp.Body)
	if err != nil {
		return err
	}
	resp.Body.Close()
	rewritten := dagNatsPathRe.ReplaceAllStringFunc(string(body), func(m string) string {
		// Re-run a simple capture to prefix only the path part.
		sub := dagNatsPathRe.FindStringSubmatch(m)
		//nolint:mnd // 4 is the minimum match group size for the regex
		if len(sub) < 4 {
			return m
		}
		quote, path, tail := sub[1], sub[2], sub[3]
		return quote + "/dagnats" + path + tail
	})
	resp.Body = io.NopCloser(bytes.NewReader([]byte(rewritten)))
	resp.ContentLength = int64(len(rewritten))
	resp.Header.Set("Content-Length", itoa(len(rewritten)))
	return nil
}

func itoa(n int) string {
	if n == 0 {
		return "0"
	}
	neg := n < 0
	if neg {
		n = -n
	}
	var b [20]byte
	i := len(b)
	for n > 0 {
		i--
		b[i] = byte('0' + n%10)
		n /= 10
	}
	if neg {
		i--
		b[i] = '-'
	}
	return string(b[i:])
}
