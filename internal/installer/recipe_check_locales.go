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
	"fmt"
	"path/filepath"
	"regexp"
	"slices"
	"strings"

	"github.com/calionauta/gogogo/internal/recipe"
)

// recipeLocalePath matches a receipt file under locale/<tag>.json.
var recipeLocalePath = regexp.MustCompile(`(?:^|/)locale/([^/]+)\.json$`)

// recipeCheckLocales is the locale half of the report: what the instance
// requires, what the recipe ships, and what is missing — for the UI catalog and
// for the declaration's own label translations, because they are two different
// files with two different jobs (a page string vs a metric label).
type recipeCheckLocales struct {
	Required []string `json:"required"`
	Shipped  []string `json:"shipped,omitempty"`
	Missing  []string `json:"missing,omitempty"`
	// Declared lists the tags the DECLARATION translates (metric labels).
	Declared []string `json:"declared,omitempty"`
	// MissingDeclared lists required tags the declaration does not translate.
	MissingDeclared []string `json:"missingDeclared,omitempty"`
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
	// A declaration that translates ANY label must translate every declared
	// language: a half-translated metric grid is the exact failure this check
	// exists to make visible. A declaration with no translations at all is a
	// different fact (the author wrote one language) and is reported as a
	// warning by localeProblems.
	if len(r.Declaration.Translations) > 0 {
		for _, t := range r.Declaration.Translations {
			out.Declared = append(out.Declared, t.Locale)
		}
		slices.Sort(out.Declared)
		for _, tag := range required {
			// The declared label IS the English spelling, so an English tag
			// needs no entry: requiring one would make every author restate
			// it, and "en" vs "en-US" is the same language written twice.
			// Every other shipped language must be spelled out, or the grid
			// mixes languages.
			if isEnglishTag(tag) {
				continue
			}
			if !slices.ContainsFunc(out.Declared, func(v string) bool { return strings.EqualFold(v, tag) }) {
				out.MissingDeclared = append(out.MissingDeclared, tag)
			}
		}
	}
	return out
}

// localeProblems turns a coverage result into report problems. A partial
// catalog is always an error; a recipe with no catalog is a warning unless the
// operator asked for strict coverage.
func localeProblems(id string, locales recipeCheckLocales, strict bool) (errs, warns []string) {
	if len(locales.MissingDeclared) > 0 {
		errs = append(errs, fmt.Sprintf(
			"recipe %s translates its metric labels for %s but not %s; a metric grid must not mix languages",
			id, strings.Join(locales.Declared, ", "), strings.Join(locales.MissingDeclared, ", ")))
	}
	if len(locales.Missing) == 0 {
		return errs, nil
	}
	missing := strings.Join(locales.Missing, ", ")
	if len(locales.Shipped) == 0 {
		msg := "recipe " + id + " ships no locale catalog; it renders core keys only (missing " + missing + ")"
		if strict {
			return append(errs, msg), warns
		}
		return errs, append(warns, msg)
	}
	// append, not replace: the declaration gap above is a separate finding and
	// dropping it would hide the half that lives in YAML.
	return append(errs, "recipe "+id+" ships locale catalogs but misses "+missing+
		" (required: "+strings.Join(locales.Required, ", ")+")"), warns
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

// isEnglishTag reports an English tag in any form ("en", "en-US", "en-GB").
func isEnglishTag(tag string) bool {
	base, _, _ := strings.Cut(strings.ToLower(tag), "-")
	return base == "en"
}
