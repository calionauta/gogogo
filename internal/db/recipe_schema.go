// The mirrored DDL of installed recipes, readable by tests.
//
// `gogogo recipe install` writes each recipe's migrations here as
// `<id>__<version>.sql` so sqlc (which reads only the top level of a schema
// entry) describes the tables the app actually runs. A test that wants a
// migrated slice used to import the slice itself just to reach its embedded
// DDL; that import is what kept the reference slice undeletable, because shared
// infrastructure then named a removable unit.
//
// This file is the seam instead: one filesystem, no feature import, and the
// bytes are the same ones the installer wrote for codegen.
package db

import (
	"embed"
	"errors"
	"io/fs"
	"strings"
)

// The embed needs at least one match at build time, and a tree that dropped
// every slice has none — so the pattern allows an empty glob rather than
// failing the build of a product that deliberately carries no recipe schema.
//
//go:embed all:recipe_schema
var recipeSchemaFS embed.FS

// RecipeSchema returns a filesystem rooted at the mirrored DDL, with the files
// renamed from `<id>__<version>.sql` to `<version>.sql` so a single set can be
// applied through dbschema.MigrateWith under that id.
//
// A missing id yields an fs with no files, which migrates nothing — the honest
// answer for a slice this tree does not carry.
func RecipeSchema(id string) fs.FS {
	prefix := id + "__"
	return recipeSchemaSubtree{prefix: prefix}
}

// recipeSchemaSubtree exposes one recipe's files under their bare version names.
type recipeSchemaSubtree struct{ prefix string }

// Open implements fs.FS: the caller asks for "0001_init.sql", the embed holds
// "todo__0001_init.sql".
func (r recipeSchemaSubtree) Open(name string) (fs.File, error) {
	f, err := recipeSchemaFS.Open("recipe_schema/" + r.prefix + strings.TrimPrefix(name, "./"))
	if err != nil && errors.Is(err, fs.ErrNotExist) {
		return nil, &fs.PathError{Op: "open", Path: name, Err: fs.ErrNotExist}
	}
	return f, err
}

// ReadDir lists one recipe's versions, stripping the prefix.
// ReadDir ignores its argument on purpose: the subtree is already rooted at
// one recipe, and the only directory a caller can name is ".".
func (r recipeSchemaSubtree) ReadDir(_ string) ([]fs.DirEntry, error) {
	entries, err := fs.ReadDir(recipeSchemaFS, "recipe_schema")
	if err != nil {
		if errors.Is(err, fs.ErrNotExist) {
			return nil, nil
		}
		return nil, err
	}
	out := make([]fs.DirEntry, 0, len(entries))
	for _, e := range entries {
		if !strings.HasPrefix(e.Name(), r.prefix) {
			continue
		}
		out = append(out, renamedEntry{DirEntry: e, name: strings.TrimPrefix(e.Name(), r.prefix)})
	}
	return out, nil
}

// ReadFile reads one version by its bare name.
func (r recipeSchemaSubtree) ReadFile(name string) ([]byte, error) {
	return fs.ReadFile(recipeSchemaFS, "recipe_schema/"+r.prefix+strings.TrimPrefix(name, "./"))
}

// renamedEntry reports a file under the name the caller asked for.
type renamedEntry struct {
	fs.DirEntry
	name string
}

func (e renamedEntry) Name() string { return e.name }
