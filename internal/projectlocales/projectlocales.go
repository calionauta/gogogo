// The languages a product ships, recorded once and read by everyone.
//
// WHY A FILE AND NOT ONLY AN ENV VAR. `SUPPORTED_LOCALES` answers the runtime
// question, but an operator's choice has to survive a new shell and travel with
// the tree: the CLI writes the record, the check compares recipes against it,
// and the boot reads it when the env var is unset. Env still wins, because a
// deployment must be able to override without editing a tracked file.
//
// WHY NOT `.gogogo/`. That directory is ignored on purpose (agent stamps and
// machine-local records). This is a product decision, so it is tracked and sits
// at the repo root where the operator can review it in a diff.
package projectlocales

import (
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"strings"

	"gopkg.in/yaml.v3"
)

// File is the tracked record, repo-relative.
const File = "locales.yml"

// recordMode is a tracked configuration file: readable, not executable.
const recordMode = 0o644

// Default is the set every tree starts with: the template's own example. A
// product that wants English only says so and the record shrinks.
var Default = []string{"en", "pt-BR"}

// Record is the on-disk shape. One field, because a second list would be a
// second answer to the same question.
type Record struct {
	// Locales are the tags the product ships, in the order a picker shows them.
	// The first is the default the fallback resolves to.
	Locales []string `yaml:"locales"`
}

// Load reads the record at root. A missing file is not an error: it means the
// tree predates the record, and the caller falls back (env, then Default).
func Load(root string) (Record, bool, error) {
	raw, err := os.ReadFile(filepath.Join(root, File))
	if err != nil {
		if errors.Is(err, os.ErrNotExist) {
			return Record{}, false, nil
		}
		return Record{}, false, err
	}
	var rec Record
	if uerr := yaml.Unmarshal(raw, &rec); uerr != nil {
		return Record{}, false, fmt.Errorf("%s: %w", File, uerr)
	}
	tags, terr := Normalize(rec.Locales)
	if terr != nil {
		return Record{}, false, fmt.Errorf("%s: %w", File, terr)
	}
	rec.Locales = tags
	return rec, true, nil
}

// Write records the tags at root, normalized and validated.
func Write(root string, tags []string) (Record, error) {
	norm, err := Normalize(tags)
	if err != nil {
		return Record{}, err
	}
	rec := Record{Locales: norm}
	body, merr := yaml.Marshal(rec)
	if merr != nil {
		return Record{}, merr
	}
	header := "# The languages this product ships — read by the boot, the CLI and\n" +
		"# `make check-locales`. The first entry is the fallback.\n"
	path := filepath.Join(root, File)
	content := []byte(header + string(body))
	if werr := os.WriteFile(path, content, recordMode); werr != nil {
		return Record{}, werr
	}
	return rec, nil
}

// Normalize trims, de-duplicates and refuses an empty set. It does not judge
// the tag's grammar: `internal/i18n` owns tag parsing, and a project record
// must be readable before the plugin is loaded.
func Normalize(tags []string) ([]string, error) {
	var out []string
	seen := map[string]bool{}
	for _, tag := range tags {
		tag = strings.TrimSpace(tag)
		if tag == "" {
			continue
		}
		if seen[strings.ToLower(tag)] {
			continue
		}
		seen[strings.ToLower(tag)] = true
		out = append(out, tag)
	}
	if len(out) == 0 {
		return nil, errors.New("no locales: a product ships at least English")
	}
	if !containsFold(out, "en") {
		out = append([]string{"en"}, out...)
	}
	return out, nil
}

// Resolve answers the configured set for a tree: the record when present, then
// SUPPORTED_LOCALES, then Default. Precedence is documented, not inferred.
func Resolve(root, envRaw string) ([]string, string) {
	if rec, ok, err := Load(root); err == nil && ok {
		return rec.Locales, File
	}
	if tags, err := Normalize(splitCSV(envRaw)); err == nil && strings.TrimSpace(envRaw) != "" {
		return tags, "SUPPORTED_LOCALES"
	}
	return append([]string(nil), Default...), "default"
}

func splitCSV(raw string) []string {
	var out []string
	for tag := range strings.SplitSeq(raw, ",") {
		out = append(out, tag)
	}
	return out
}

func containsFold(list []string, want string) bool {
	for _, v := range list {
		if strings.EqualFold(v, want) {
			return true
		}
	}
	return false
}
