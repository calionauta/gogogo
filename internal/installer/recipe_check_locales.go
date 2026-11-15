// Locale coverage for a checked recipe.
//
// A recipe ships its own strings under locale/<tag>.json. A missing tag falls
// back to the default locale at runtime, which is the right behaviour and the
// reason the gap is invisible: a Portuguese page shows an English word and
// nothing reports it. This check makes the gap visible.
//
// Two grades, because a recipe with no catalog yet is a different fact from a
// recipe that translated half a page:
//   - ships no locale catalog  -> warning (renders core keys only)
//   - ships some, misses a tag -> error (partial translation)
//
// The second is what the operator's SUPPORTED_LOCALES makes non-negotiable.
package installer

import (
	"path/filepath"
	"regexp"
	"slices"
	"strings"

	"github.com/calionauta/gogogo/internal/recipe"
)

// recipeLocalePath matches a receipt file under locale/<tag>.json.
var recipeLocalePath = regexp.MustCompile(`(?:^|/)locale/([^/]+)\.json$`)

// recipeCheckLocales is the locale half of the report: what the instance
// requires, what the recipe ships, and what is missing.
type recipeCheckLocales struct {
	Required []string `json:"required"`
	Shipped  []string `json:"shipped,omitempty"`
	Missing  []string `json:"missing,omitempty"`
}

// checkRecipeLocales grades one recipe against the instance's supported tags.
func checkRecipeLocales(root string, r recipe.Receipt, required []string) recipeCheckLocales {
	out := recipeCheckLocales{Required: append([]string(nil), required...)}
	out.Shipped = shippedLocales(root, r)
	for _, tag := range required {
		if !slices.Contains(out.Shipped, tag) {
			out.Missing = append(out.Missing, tag)
		}
	}
	slices.Sort(out.Shipped)
	return out
}

// localeProblems turns a coverage result into report problems. A partial
// catalog is always an error; a recipe with no catalog is a warning unless the
// operator asked for strict coverage.
func localeProblems(id string, locales recipeCheckLocales, strict bool) (errs, warns []string) {
	if len(locales.Missing) == 0 {
		return nil, nil
	}
	missing := strings.Join(locales.Missing, ", ")
	if len(locales.Shipped) == 0 {
		msg := "recipe " + id + " ships no locale catalog; it renders core keys only (missing " + missing + ")"
		if strict {
			return []string{msg}, nil
		}
		return nil, []string{msg}
	}
	return []string{"recipe " + id + " ships locale catalogs but misses " + missing +
		" (required: " + strings.Join(locales.Required, ", ") + ")"}, nil
}

// shippedLocales reads the tags a receipt carries, with a filesystem fallback
// for a recipe whose receipt predates its locale files.
func shippedLocales(root string, r recipe.Receipt) []string {
	seen := map[string]bool{}
	for p := range r.Files {
		if m := recipeLocalePath.FindStringSubmatch(strings.ReplaceAll(p, "\\", "/")); m != nil {
			seen[m[1]] = true
		}
	}
	// The receipt is the source of truth, but a scaffold may add locale files
	// before the receipt is rewritten. Read the conventional directory too.
	matches, _ := filepath.Glob(filepath.Join(root, "features", r.ID, "locale", "*.json"))
	for _, p := range matches {
		seen[strings.TrimSuffix(filepath.Base(p), ".json")] = true
	}
	out := make([]string, 0, len(seen))
	for tag := range seen {
		out = append(out, tag)
	}
	return out
}
