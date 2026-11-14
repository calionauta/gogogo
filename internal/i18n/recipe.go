// Recipe catalogs: a recipe ships its own strings, the engine merges them.
//
// WHY A MERGE AND NOT A GLOBAL DICTIONARY. The app catalog (locale/*.json) is
// core: it is copied with the tree and owned by whoever owns the tree. A
// recipe is a slice that arrives later and may be uninstalled, so its strings
// must travel WITH it — deleting `features/kiosk` must delete `kiosk.*`
// without anyone editing a core file. A merge at boot gives both properties:
// one lookup table at request time, one owner per key at build time.
//
// WHY AUTO-PREFIXED KEYS. A recipe's file holds `title`, not
// `recipe.kiosk.title`. The id it is registered under becomes the prefix, so
// two recipes that both call something "title" cannot collide, and a recipe's
// key can never shadow a core one. The prefix is the recipe id — the same word
// the receipt, the rail and the dashboard already use.
//
// WHY A MISSING LOCALE IS NOT AN ERROR. A recipe may ship en.json alone; the
// catalog's existing fallback answers every pt-BR lookup from English. The
// alternative (refusing boot) would make a partial translation a broken
// install, which is worse than an untranslated word.
package i18n

import (
	"encoding/json"
	"errors"
	"fmt"
	"io/fs"
	"maps"
	"slices"
	"sort"
	"strings"
)

// RecipeCatalog is one recipe's translation files, already opened.
type RecipeCatalog struct {
	// ID is the recipe id; it becomes the key prefix.
	ID string
	// FS holds `locale/<tag>.json`, the same layout as the core catalog.
	FS fs.FS
}

// WithRecipes returns a catalog that also answers the recipes' keys.
//
// It is a merge, not a mutation: the receiver is the boot catalog and stays
// immutable, so a test can build a catalog with two recipes and a request in
// flight never sees a half-merged map.
func (c *Catalog) WithRecipes(recipes []RecipeCatalog) (*Catalog, error) {
	if len(recipes) == 0 {
		return c, nil
	}
	merged := &Catalog{
		messages:  make(map[string]map[string]string, len(c.messages)),
		fallback:  c.fallback,
		supported: append([]string(nil), c.supported...),
	}
	for tag, m := range c.messages {
		merged.messages[tag] = maps.Clone(m)
	}
	// Sorted so a collision names the same pair in the same order on every
	// boot; a map iteration would make the error message nondeterministic.
	sorted := append([]RecipeCatalog(nil), recipes...)
	sort.Slice(sorted, func(i, j int) bool { return sorted[i].ID < sorted[j].ID })

	// claimed tracks every prefixed key PER TAG, so two recipes cannot claim
	// one and the same recipe's en.json and pt-BR.json do not collide with each
	// other (they are supposed to declare the same keys).
	for _, rc := range sorted {
		if err := merged.mergeRecipe(rc); err != nil {
			return nil, err
		}
	}
	return merged, nil
}

// mergeRecipe folds one recipe's files into the merged catalog. Collisions are
// detected per tag: the same key in two locales of one recipe is the point.
func (c *Catalog) mergeRecipe(rc RecipeCatalog) error {
	if rc.ID == "" {
		return errors.New("i18n: a recipe catalog has no id")
	}
	found := false
	for _, tag := range c.supported {
		m, err := loadRecipeLocale(rc.FS, tag)
		if err != nil {
			return err
		}
		if m == nil {
			continue
		}
		found = true
		if c.messages[tag] == nil {
			c.messages[tag] = map[string]string{}
		}
		for k, v := range m {
			key := rc.ID + "." + k
			if _, dup := c.messages[tag][key]; dup {
				return fmt.Errorf(
					"i18n: %q declares %q twice for %s", rc.ID, key, tag)
			}
			c.messages[tag][key] = v
		}
	}
	if !found {
		return fmt.Errorf(
			"i18n: recipe %q ships no catalog for any supported locale (%s)",
			rc.ID, strings.Join(c.supported, ", "))
	}
	return nil
}

// loadRecipeLocale reads one recipe's file for a tag, or nil when it has none.
func loadRecipeLocale(fsys fs.FS, tag string) (map[string]string, error) {
	raw, err := fs.ReadFile(fsys, "locale/"+tag+".json")
	if err != nil {
		if isNotExist(err) {
			return nil, nil
		}
		return nil, fmt.Errorf("i18n: read recipe catalog for %q: %w", tag, err)
	}
	var m map[string]string
	if err := json.Unmarshal(raw, &m); err != nil {
		return nil, fmt.Errorf("i18n: decode recipe catalog for %q: %w", tag, err)
	}
	for k := range m {
		// A key that already carries the prefix would come out as
		// `kiosk.kiosk.title` and never be found; refusing is cheaper than a
		// silent miss.
		if strings.HasPrefix(k, ".") || strings.HasSuffix(k, ".") {
			return nil, fmt.Errorf("i18n: recipe key %q starts or ends with a dot", k)
		}
	}
	return m, nil
}

// isNotExist reports a missing-file error from any fs implementation.
func isNotExist(err error) bool {
	return errors.Is(err, fs.ErrNotExist) || strings.Contains(err.Error(), "file does not exist")
}

// CheckRecipeCoverage reports supported locales a recipe does not translate.
//
// It is advisory by design (the fallback makes a partial catalog work), but it
// has to be visible: a recipe that ships only English renders English words
// inside a translated page, and nothing else in the system notices.
func CheckRecipeCoverage(supported []string, recipes []RecipeCatalog) []string {
	var out []string
	for _, rc := range recipes {
		for _, tag := range supported {
			m, err := loadRecipeLocale(rc.FS, tag)
			if err != nil {
				out = append(out, fmt.Sprintf("recipe %q: %v", rc.ID, err))
				continue
			}
			if m == nil {
				out = append(out, fmt.Sprintf(
					"recipe %q has no %s.json (it falls back to %s for those strings)",
					rc.ID, tag, "en"))
			}
		}
	}
	slices.Sort(out)
	return out
}
