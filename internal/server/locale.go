// The locale preference endpoint, the write half of internal/i18n.
//
// i18n.Resolve already reads a preference in this order: ?lang=, the cookie,
// Accept-Language, then the fallback. Nothing wrote that cookie, so a user
// could only pick a language by editing a URL — which is not an affordance.
// This is the one handler that sets it.
//
// It mirrors demoModeHandler on purpose (same shape, same guards): a cookie is
// the only state, the browser navigates back, and there is no per-user record.
// The locale is a presentation preference, not identity, so it stays readable
// and cookie-only until something needs it server-side per account.
package server

import (
	"fmt"
	"log/slog"
	"net/http"
	"net/url"
	"strings"

	"github.com/calionauta/gogogo/internal/i18n"
)

// localeHandler sets the locale cookie from ?lang= and redirects to ?next=.
//
// Unsupported tags are refused (400), not silently ignored: a link that appears
// to work and changes nothing is worse than a clear refusal. The supported set
// is the boot catalog's, so the handler cannot offer a language the templates
// have no strings for.
func localeHandler(cat *i18n.Catalog) http.HandlerFunc {
	supported := []string{"en"}
	fallback := "en"
	if cat != nil {
		supported = cat.Supported()
		fallback = cat.Fallback()
	}
	return func(w http.ResponseWriter, r *http.Request) {
		next := sameOriginPath(backTarget(r))
		want := strings.TrimSpace(r.URL.Query().Get("lang"))
		tag, ok := matchSupported(want, supported)
		if !ok {
			http.Error(w, fmt.Sprintf("unsupported language %q (want %s)",
				want, strings.Join(supported, ", ")), http.StatusBadRequest)
			return
		}
		// HttpOnly is false ON PURPOSE: the page marks the active language from
		// the cookie, and a display preference is not a credential. Secure and
		// SameSite stay as the rest of the app.
		c := &http.Cookie{ //nolint:gosec // G124: see the comment above
			Name:     i18n.CookieName,
			Value:    tag,
			Path:     "/",
			HttpOnly: false, // readable by the page: the UI marks the active option
			SameSite: http.SameSiteLaxMode,
			Secure:   true,
		}
		if tag == fallback {
			// Choosing the default clears the cookie rather than pinning it: the
			// fallback is already what an absent preference resolves to, and a
			// pinned cookie would stop following the browser's own language.
			c.Value = ""
			c.MaxAge = -1
		}
		http.SetCookie(w, c)
		http.Redirect(w, r, next, http.StatusSeeOther)
	}
}

// backTarget is where the user returns after choosing: an explicit ?next= wins,
// otherwise the Referer's path. The Referer is a full URL, so only its path is
// kept — sameOriginPath then applies the same guard to both inputs.
func backTarget(r *http.Request) string {
	if q := r.URL.Query().Get("next"); q != "" {
		return q
	}
	ref := r.Header.Get("Referer")
	if ref == "" {
		return "/"
	}
	u, err := url.Parse(ref)
	if err != nil {
		return "/"
	}
	return u.Path
}

// sameOriginPath accepts a same-origin path and drops everything else.
//
// Parsing (rather than string-matching) drops a host, a scheme and anything a
// browser could treat as absolute; the fallback is the module root. This is the
// open-redirect guard demoModeHandler uses, kept identical so the two cannot
// drift.
func sameOriginPath(next string) string {
	u, err := url.Parse(next)
	if err != nil || u.Host != "" || u.Scheme != "" ||
		!strings.HasPrefix(u.Path, "/") || strings.HasPrefix(next, "//") {
		return "/"
	}
	return next
}

// matchSupported normalizes a tag and accepts it when it is one the catalog
// ships. Exact match only: a "pt" that silently became "pt-BR" would hide a
// broken link.
func matchSupported(raw string, supported []string) (string, bool) {
	for _, tag := range supported {
		if strings.EqualFold(raw, tag) {
			slog.Debug("server: locale selected", "tag", tag)
			return tag, true
		}
	}
	return "", false
}
