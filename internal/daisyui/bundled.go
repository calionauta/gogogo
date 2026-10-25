// SCOPE:core — the bundled DaisyUI vocabulary (generated, never hand-edited).
//
// Why a snapshot lives in the binary: a project that is NOT gogogo has no
// vendored skill, so `daisyui-lint` would be unusable there without a file the
// user does not have. `internal/daisyui/vocabulary.json` is generated from the
// skill (`make vocabulary`) and embedded, so the standalone binary works out of
// the box; `--skill` still wins when a project pins its own copy.
//
// The 2026 evidence for this shape: committed generated artifacts drift
// silently unless CI regenerates them (Safeguard, Oct 2026: "commit it and have
// CI check that regeneration produces no difference"), and a linter whose
// vocabulary is hand-maintained goes stale until a user reports a false
// positive (claudelint's `specdrift` design). So the snapshot is committed AND
// `TestBundledVocabularyMatchesSkill` regenerates it from the vendored skill and
// fails on any difference — the same rule the repo applies to `_templ.go` and
// `app.min.css`.
package daisyui

import (
	_ "embed"
	"encoding/json"
	"errors"
	"fmt"
	"sort"
)

//go:embed vocabulary.json
var bundledJSON []byte

// bundledFile is the on-disk shape: sorted arrays so the diff of a regeneration
// is reviewable.
type bundledFile struct {
	SkillVersion string         `json:"skillVersion"`
	Components   []bundledEntry `json:"components"`
}

type bundledEntry struct {
	Base    string   `json:"base"`
	Name    string   `json:"name"`
	Hosts   []string `json:"hosts,omitempty"`
	Classes []string `json:"classes"`
	Parts   []string `json:"parts,omitempty"`
	Sizes   []string `json:"sizes,omitempty"`
	Colors  []string `json:"colors,omitempty"`
}

// LoadBundled returns the embedded vocabulary and the DaisyUI version it was
// generated from.
func LoadBundled() (Vocabulary, string, error) {
	var file bundledFile
	if err := json.Unmarshal(bundledJSON, &file); err != nil {
		return nil, "", fmt.Errorf("bundled vocabulary is not valid JSON: %w", err)
	}
	vocab := make(Vocabulary, len(file.Components))
	for _, e := range file.Components {
		vocab[e.Base] = &Component{
			Name:    e.Name,
			Base:    e.Base,
			Hosts:   setOf(e.Hosts),
			Classes: setOf(e.Classes),
			Parts:   setOf(e.Parts),
			Sizes:   setOf(e.Sizes),
			Colors:  setOf(e.Colors),
		}
	}
	if len(vocab) == 0 {
		return nil, "", errors.New("bundled vocabulary is empty (run `make vocabulary`)")
	}
	return vocab, file.SkillVersion, nil
}

// VocabularyJSON renders a vocabulary in the bundled shape, deterministically.
// The generator writes it to internal/daisyui/vocabulary.json; the test compares
// that file with a fresh render of the vendored skill.
func VocabularyJSON(vocab Vocabulary, skillVersion string) ([]byte, error) {
	file := bundledFile{SkillVersion: skillVersion}
	for _, spec := range vocab {
		file.Components = append(file.Components, bundledEntry{
			Base:    spec.Base,
			Name:    spec.Name,
			Hosts:   sortedKeys(spec.Hosts),
			Classes: sortedKeys(spec.Classes),
			Parts:   sortedKeys(spec.Parts),
			Sizes:   sortedKeys(spec.Sizes),
			Colors:  sortedKeys(spec.Colors),
		})
	}
	sort.Slice(file.Components, func(i, j int) bool { return file.Components[i].Base < file.Components[j].Base })
	raw, err := json.MarshalIndent(file, "", "  ")
	if err != nil {
		return nil, err
	}
	return append(raw, '\n'), nil
}

func setOf(items []string) map[string]bool {
	out := make(map[string]bool, len(items))
	for _, it := range items {
		out[it] = true
	}
	return out
}
