// The translation half of the seam: a recipe ships its own strings, beside its
// own DDL.
//
// Why this is a THIRD optional interface instead of a method on Provider: most
// recipes have no strings of their own (they render core components), and
// widening Provider would make every one of them implement a method returning
// nil. The same reasoning that put Migrator here applies: a small promise for
// the units that answer it, and a nil answer that means "I bring no strings"
// rather than an error.
//
// The layout is the core catalog's (`locale/<tag>.json`, a flat map of key to
// string), and the keys are UNPREFIXED: the engine prefixes them with the
// recipe id, so `title` in the kiosk recipe becomes `kiosk.title`. That is what
// lets a recipe say the obvious thing and still cannot collide with core or
// with another recipe.
package recipe

import "io/fs"

// Translator is what a recipe with its own strings implements.
type Translator interface {
	// Translations returns a filesystem holding `locale/<tag>.json`. The usual
	// shape is an embed.FS with that subdirectory. Nil (or a filesystem with no
	// locale directory) means this recipe brings no strings, which is valid:
	// its markup then renders core keys only.
	Translations() fs.FS
}

// TranslationSets collects the recipes that ship strings, in ID order so a boot
// merge is deterministic and a collision names the same pair every time.
//
// It returns the value rather than an import of internal/i18n: this package
// must not depend on the localization servant, which is removable. The caller
// (the composition root) is the only place that knows both.
type TranslationSet struct {
	// ID is the recipe id, used as the key prefix.
	ID string
	// FS holds `locale/<tag>.json`.
	FS fs.FS
}

// TranslationSets returns the translation filesystems of the registered
// recipes that declare one, in ID order.
func (r *Registry) TranslationSets() []TranslationSet {
	out := make([]TranslationSet, 0, len(r.providers))
	for _, p := range r.Providers() {
		t, ok := p.(Translator)
		if !ok {
			continue
		}
		fsys := t.Translations()
		if fsys == nil {
			continue
		}
		out = append(out, TranslationSet{ID: p.ID(), FS: fsys})
	}
	return out
}
