package i18n

import (
	"context"
	"fmt"
	"maps"
	"net/http"
	"strings"
	"time"

	"golang.org/x/text/language"
)

// Translator is the only surface features call. It resolves
// the request locale from context and renders from the
// catalog. Implementations never read the network, write
// cookies, or hold per-request state.
type Translator interface {
	T(ctx context.Context, id string, args map[string]any) string
	TCount(ctx context.Context, id string, count int, args map[string]any) string
	Num(ctx context.Context, f float64, prec int) string
	Date(ctx context.Context, t time.Time, style Style) string
}

// Catalog renders through its immutable messages. Resolve
// the locale alongside the catalog; helpers below carry it
// through a request context.
func (c *Catalog) T(ctx context.Context, id string, args map[string]any) string {
	return c.render(Locale(ctx), id, args)
}

// TCount picks the singular form when count is one and the
// plural form otherwise. English and Brazilian Portuguese
// need only that split in phase one; irregular plural
// languages are the phase-two trigger for CLDR depth.
func (c *Catalog) TCount(ctx context.Context, id string, count int, args map[string]any) string {
	merged := mergeCount(args, count)
	form := id + ".other"
	if count == 1 {
		form = id + ".one"
	}
	if s, ok := c.Lookup(Locale(ctx), form); ok {
		return interpolate(s, merged)
	}
	return c.render(Locale(ctx), id, merged)
}

func (c *Catalog) render(tag, id string, args map[string]any) string {
	s, ok := c.Lookup(tag, id)
	if !ok {
		return id
	}
	return interpolate(s, args)
}

// interpolate replaces {{.Name}} placeholders with the
// matching argument. It never executes template code:
// anything that is not exactly {{.Name}} stays literal.
func interpolate(s string, args map[string]any) string {
	if len(args) == 0 || !strings.Contains(s, "{{") {
		return s
	}
	var b strings.Builder
	b.Grow(len(s))
	i := 0
	for i < len(s) {
		if name, width, ok := placeholderAt(s[i:]); ok {
			if v, ok := args[name]; ok {
				_, _ = fmt.Fprint(&b, v)
				i += width
				continue
			}
		}
		b.WriteByte(s[i])
		i++
	}
	return b.String()
}

// placeholderAt reports the name and width of a {{.Name}}
// placeholder at the start of rest, if present.
func placeholderAt(rest string) (string, int, bool) {
	if len(rest) < 5 || rest[0] != '{' || rest[1] != '{' {
		return "", 0, false
	}
	end := strings.Index(rest[2:], "}}")
	if end < 0 {
		return "", 0, false
	}
	inner := strings.TrimSpace(rest[2 : 2+end])
	name, ok := strings.CutPrefix(inner, ".")
	if !ok || name == "" {
		return "", 0, false
	}
	return name, 2 + end + 2, true
}

func mergeCount(args map[string]any, count int) map[string]any {
	if args == nil {
		return map[string]any{"Count": count}
	}
	out := make(map[string]any, len(args)+1)
	maps.Copy(out, args)
	if _, ok := out["Count"]; !ok {
		out["Count"] = count
	}
	return out
}

// Locale returns the resolved tag from ctx, or the fallback
// when none was set.
func Locale(ctx context.Context) string {
	if v, ok := ctx.Value(ctxKey{}).(string); ok && v != "" {
		return v
	}
	return DefaultLocale
}

// WithLocale stores tag in ctx. Handlers and components
// call Locale(ctx) or the Translator methods, never the
// cookie or the header directly.
func WithLocale(ctx context.Context, tag string) context.Context {
	parsed, err := language.Parse(tag)
	if err == nil {
		tag = parsed.String()
	}
	return context.WithValue(ctx, ctxKey{}, tag)
}

type ctxKey struct{}

type catalogKey struct{}

// DefaultLocale is used when a context carries no locale.
const DefaultLocale = "en"

// WithCatalog stores catalog in ctx. Templates call T and
// TCount with the request ctx; they never hold a catalog,
// read a cookie, or know which tag won.
func WithCatalog(ctx context.Context, c *Catalog) context.Context {
	return context.WithValue(ctx, catalogKey{}, c)
}

// CatalogFromContext returns the ctx catalog, or the
// embedded default when the request carried none (the
// disabled-plugin path, where markup stays English).
func CatalogFromContext(ctx context.Context) *Catalog {
	if v, ok := ctx.Value(catalogKey{}).(*Catalog); ok && v != nil {
		return v
	}
	return Default()
}

// CatalogInContext returns the ctx catalog and whether one was installed.
//
// It exists for the ONE caller that must distinguish "the plugin is off" from
// "the plugin is on and this is the default catalog": a language picker. With
// the servant off nothing resolves a locale per request, so offering the choice
// would be an affordance the server does not honour. CatalogFromContext keeps
// its fallback for rendering, where English-is-fine is the correct answer.
func CatalogInContext(ctx context.Context) (*Catalog, bool) {
	v, ok := ctx.Value(catalogKey{}).(*Catalog)
	return v, ok && v != nil
}

// T renders id for the request locale.
func T(ctx context.Context, id string, args map[string]any) string {
	return CatalogFromContext(ctx).T(ctx, id, args)
}

// TCount renders the plural form of id for count.
func TCount(ctx context.Context, id string, count int, args map[string]any) string {
	return CatalogFromContext(ctx).TCount(ctx, id, count, args)
}

// Middleware resolves the request locale and stores it in
// the request context. It only reads: query, cookie, and
// Accept-Language. It sets no cookie and never blocks a
// request; an unsupported preference falls back.
func Middleware(supported []string, fallback string, next http.Handler) http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		tag := Resolve(r, supported, fallback)
		next.ServeHTTP(w, r.WithContext(WithLocale(r.Context(), tag)))
	})
}

// CatalogMiddleware is Middleware plus catalog injection:
// handlers and components read the request catalog from ctx
// and never receive it as a constructor arg.
func CatalogMiddleware(cat *Catalog, supported []string, fallback string, next http.Handler) http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		tag := Resolve(r, supported, fallback)
		ctx := WithCatalog(WithLocale(r.Context(), tag), cat)
		next.ServeHTTP(w, r.WithContext(ctx))
	})
}
