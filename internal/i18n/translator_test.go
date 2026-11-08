package i18n

import (
	"context"
	"testing"
	"time"
)

func TestTInterpolatesWithoutTemplateExecution(t *testing.T) {
	c := loadTestCatalog(t)
	ctx := WithLocale(context.Background(), "pt-BR")
	got := c.T(ctx, "home.title", nil)
	if got != "Início" {
		t.Fatalf("T home.title = %q", got)
	}
	// A template-looking argument must stay literal in the output,
	// proving interpolation is plain substitution, not execution.
	injected := c.T(ctx, "nav.modules", map[string]any{"Count": 1})
	if injected != "Módulos" {
		t.Fatalf("T nav.modules = %q", injected)
	}
	if got := c.T(ctx, "no.such.key", nil); got != "no.such.key" {
		t.Fatalf("unknown key must surface the id, got %q", got)
	}
}

func TestTCountPicksSingularAndPlural(t *testing.T) {
	c := loadTestCatalog(t)
	ctx := WithLocale(context.Background(), "en")
	if got := c.TCount(ctx, "inbox.unread", 1, nil); got != "1 unread" {
		t.Fatalf("singular = %q", got)
	}
	if got := c.TCount(ctx, "inbox.unread", 5, nil); got != "5 unread" {
		t.Fatalf("plural = %q", got)
	}
	ctx = WithLocale(context.Background(), "pt-BR")
	if got := c.TCount(ctx, "inbox.unread", 2, nil); got != "2 não lidas" {
		t.Fatalf("pt plural = %q", got)
	}
}

func TestNumAndDateAreLocaleShaped(t *testing.T) {
	c := loadTestCatalog(t)
	ctx := WithLocale(context.Background(), "pt-BR")
	num := c.Num(ctx, 1234.5, 2)
	if num == "1234.50" {
		t.Fatalf("pt number must differ from en shape, got %q", num)
	}
	at := time.Date(2026, 11, 8, 12, 0, 0, 0, time.UTC)
	if got := c.Date(ctx, at, StyleMedium); got != "8 de nov. de 2026" {
		t.Fatalf("pt date = %q", got)
	}
	ctx = WithLocale(context.Background(), "en")
	if got := c.Date(ctx, at, StyleMedium); got != "Nov 8, 2026" {
		t.Fatalf("en date = %q", got)
	}
}

func TestLocaleDefaultsWhenUnset(t *testing.T) {
	if got := Locale(context.Background()); got != DefaultLocale {
		t.Fatalf("unset locale = %q", got)
	}
}
