// SCOPE:core — The demo-mode toggle route.
//
// Demo mode decides whether the state panel is docked. It is a session cookie
// set here, not a query param and not localStorage: the mode has to follow the
// session across modules and survive a reload, and the server has to know it
// before it renders the page (a script deciding the layout after paint is the
// flash this avoids).
//
// It is a GET so the toggle in the header is a plain link, and it is public: it
// changes a display preference and nothing else. `next` is validated to a
// same-origin path — an unvalidated one would make this an open redirect.
package server

import (
	"net/http"
	"net/url"
	"strings"

	"github.com/calionauta/gogogo/internal/demo"
)

// demoModeHandler sets or clears the demo cookie and sends the visitor back to
// the page they were on.
func demoModeHandler(w http.ResponseWriter, r *http.Request) {
	next := r.URL.Query().Get("next")
	// Same-origin path only. Parsing (rather than string-matching) drops a host,
	// a scheme, and anything a browser could treat as absolute; the fallback is
	// the module root.
	if u, err := url.Parse(next); err != nil || u.Host != "" || u.Scheme != "" ||
		!strings.HasPrefix(u.Path, "/") || strings.HasPrefix(next, "//") {
		next = "/"
	}
	on := r.URL.Query().Get("on") == "1"

	c := &http.Cookie{
		Name:     demo.Cookie,
		Value:    "1",
		Path:     "/",
		HttpOnly: true,
		SameSite: http.SameSiteLaxMode,
		Secure:   true,
	}
	if !on {
		// Clearing: expire it now. No MaxAge on the "on" path, so the mode lasts
		// as long as the browser session — the same lifetime as the session it
		// belongs to.
		c.Value = ""
		c.MaxAge = -1
	}
	http.SetCookie(w, c)
	// #nosec G710 -- `next` is validated above to a same-origin path (no host, no
	// scheme, single leading slash); the gosec rule cannot follow the parse.
	http.Redirect(w, r, next, http.StatusSeeOther)
}
