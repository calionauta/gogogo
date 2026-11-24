// The document language tag follows the request locale.
package ui

import (
	"context"
	"strings"
	"testing"

	"github.com/calionauta/gogogo/internal/i18n"
)

func TestShellRendersRequestLocaleTag(t *testing.T) {
	ctx := i18n.WithLocale(context.Background(), "pt-BR")
	html := renderShell(ctx, t)
	if !strings.Contains(html, `lang="pt-BR"`) {
		t.Fatal("shell must render the request locale tag")
	}
}

// TestShellDefaultsToTheDefaultLocale: with no request locale, the document
// language is the catalog's default (the first tag of the product's record),
// not a literal the template happens to carry.
func TestShellDefaultsToTheDefaultLocale(t *testing.T) {
	html := renderShell(context.Background(), t)
	want := i18n.Locale(context.Background())
	if !strings.Contains(html, `lang="`+want+`"`) {
		t.Fatalf("shell must default to %q, got no matching lang attribute", want)
	}
}
