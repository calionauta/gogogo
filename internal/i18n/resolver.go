package i18n

import (
	"net/http"
	"slices"
	"strings"

	"golang.org/x/text/language"
)

// CookieName is the locale preference cookie. It is the only
// client-controlled locale state phase one reads; no user
// identity is attached to it.
const CookieName = "gogogo_locale"

// ParseSupported validates and normalizes comma-separated
// tags (for example "en,pt-BR"). Invalid tags are skipped;
// callers remain responsible for loading catalogs for the
// returned set.
func ParseSupported(raw string) ([]string, error) {
	var out []string
	seen := map[string]bool{}
	for part := range strings.SplitSeq(raw, ",") {
		p := strings.TrimSpace(part)
		if p == "" {
			continue
		}
		tag, err := language.Parse(p)
		if err != nil {
			return nil, err
		}
		s := tag.String()
		if seen[s] {
			continue
		}
		seen[s] = true
		out = append(out, s)
	}
	return out, nil
}

// Resolve picks the request locale: query "lang", then
// cookie, then Accept-Language, then fallback. Only tags in
// supported apply; anything else is ignored, never passed
// through as a request to load content for.
func Resolve(r *http.Request, supported []string, fallback string) string {
	if r == nil {
		return fallback
	}
	if v := strings.TrimSpace(r.URL.Query().Get("lang")); v != "" {
		if tag, ok := supportedTag(v, supported); ok {
			return tag
		}
	}
	if c, err := r.Cookie(CookieName); err == nil && strings.TrimSpace(c.Value) != "" {
		if tag, ok := supportedTag(c.Value, supported); ok {
			return tag
		}
	}
	if h := r.Header.Get("Accept-Language"); strings.TrimSpace(h) != "" {
		if tag, err := matchAccept(h, supported); err == nil && tag != "" {
			return tag
		}
	}
	return fallback
}

// supportedTag normalizes raw and reports it when it exactly
// matches a supported tag.
func supportedTag(raw string, supported []string) (string, bool) {
	tag, err := language.Parse(raw)
	if err != nil {
		return "", false
	}
	s := tag.String()
	if slices.Contains(supported, s) {
		return s, true
	}
	return "", false
}

// matchAccept matches an Accept-Language header against the
// supported set, respecting quality and region fallback.
func matchAccept(header string, supported []string) (string, error) {
	tags, _, err := language.ParseAcceptLanguage(header)
	if err != nil {
		return "", err
	}
	refs := make([]language.Tag, 0, len(supported))
	for _, s := range supported {
		tag, err := language.Parse(s)
		if err != nil {
			return "", err
		}
		refs = append(refs, tag)
	}
	tag, _, _ := language.NewMatcher(refs).Match(tags...)
	if tag == language.Und {
		return "", nil
	}
	return tag.String(), nil
}
