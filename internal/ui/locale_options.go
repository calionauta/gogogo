// Account-panel preference: the interface language.
//
// WHY HERE AND NOT THE RAIL. The rail has one job per slot already (brand,
// destinations, mute, demo toggle, account). A language picker in the rail
// would be a sixth icon competing for the same corner, and it would be visible
// on pages a guest sees before signing in — where a two-option toggle is noise.
// The account panel is where the other per-person preference already lives (the
// palette), so both preferences are in one place, next to the identity that
// says whose they are.
//
// WHY LINKS, NOT A DATASTAR POST. The choice must change the NEXT page's
// server render: the catalog is resolved per request from the cookie, so the
// server has to see the new value before it renders. A Datastar patch would
// translate nothing (the strings are already in the DOM) and would need a
// client-side dictionary — a second source of truth for every string. A link to
// /_ui/locale sets the cookie and the browser reloads the same page, which
// re-renders through the same templates. Datastar owns the things it is good at
// (the theme is a signal, it needs no round trip); language is a server
// decision, so it stays one.
//
// The list is built from the BOOT CATALOG, not a literal: an option that has no
// strings behind it would render an untranslated page and look broken. When the
// servant is off there is no catalog, so there is no picker at all — the UI
// cannot offer a choice the server will not honour.
package ui

import (
	"context"
	"strings"

	"github.com/calionauta/gogogo/internal/i18n"
)

// LocaleOption is one language in the account panel.
type LocaleOption struct {
	Tag    string // the catalog tag, e.g. "pt-BR"
	Label  string // the language's own name, never translated
	Active bool
}

// localeLabels are the endonyms. A language list in the language being left is
// the classic failure ("Portuguese" is not how a pt-BR reader looks for it);
// these are what a speaker searches for, and they stay untranslated on purpose.
var localeLabels = map[string]string{
	"en":    "English",
	"pt-BR": "Português (Brasil)",
}

// LocaleOptions builds the picker's rows from the catalog, so the options and
// the strings that exist cannot disagree.
func LocaleOptions(cat *i18n.Catalog, current string) []LocaleOption {
	if cat == nil {
		return nil
	}
	tags := cat.Supported()
	if len(tags) < 2 {
		// One language is not a choice. Rendering a single option (or a
		// disabled one) is the affordance that does nothing.
		return nil
	}
	out := make([]LocaleOption, 0, len(tags))
	for _, tag := range tags {
		label := localeLabels[tag]
		if label == "" {
			// A catalog added without an endonym still works: the tag itself is
			// readable, and refusing to render would hide a shipped language.
			label = tag
		}
		out = append(out, LocaleOption{Tag: tag, Label: label, Active: strings.EqualFold(tag, current)})
	}
	return out
}

// Href is the link that selects this language. Built here (not in the markup)
// so the template stays one attribute expression: an href whose value is itself
// a quoted string makes the datastar lint read it as a literal attribute.
func (o LocaleOption) Href() string {
	return "/_ui/locale?lang=" + o.Tag
}

// ModuleLabel is a destination's name in the request's language.
//
// The catalog owns the words (nav.<id>); the module registry owns the
// destinations. A key the catalogs do not have falls back to the registry's
// English title, which is also what the disabled-plugin path renders — so a new
// module works before anyone translates it, and the fallback is visible rather
// than a blank label.
func ModuleLabel(ctx context.Context, m Module) string {
	if s, ok := i18n.CatalogFromContext(ctx).Lookup(i18n.Locale(ctx), "nav."+m.ID); ok {
		return s
	}
	return m.Title
}
