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

func TestShellDefaultsToEnglishTag(t *testing.T) {
	html := renderShell(context.Background(), t)
	if !strings.Contains(html, `lang="en"`) {
		t.Fatal("shell must default to English")
	}
}
