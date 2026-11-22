// Trim provenance: which paths a scaffold deliberately removed.
//
// A trimmed tree keeps the registry entries of the units it dropped, because
// the registry is the template's and is compiled in. That is correct for
// `--recipes none`: a slice a kept file still imports has to stay. It is wrong
// for everything else, because a reader asking "does this tree ship chat?"
// would be told yes by a registry entry whose directories are gone.
//
// `AGENTS.md`'s `## Trim provenance` section already records the answer —
// `removed:` for the unit ids and `removed-paths:` for the directories and
// files they owned — and the scaffold writes it in the same run that deletes
// the files. Reading that record is how a gate distinguishes "deliberately
// trimmed" from "missing", without importing the installer (which would cycle:
// installer imports this package).
//
// `cmd/check-domains` and the registry conformance tests both read it. One
// reader, not two: a second parser for the same marker is precisely the drift
// these records exist to prevent.
package capabilities

import (
	"os"
	"path/filepath"
	"strings"
)

// trimProvenanceHeading is the AGENTS.md section that holds the records. A tree
// without it (the full template, or a hand-made checkout) is checked strictly.
const trimProvenanceHeading = "## Trim provenance"

// TrimProvenance is what a scaffold recorded about its own removals.
type TrimProvenance struct {
	// Recorded is true when AGENTS.md carried the section, whatever it said.
	// A recorded-but-empty provenance is a different fact from no record at
	// all, and only the second is ambiguous.
	Recorded bool
	// Removed is the unit ids the scaffold dropped (`removed:`).
	Removed map[string]bool
	// Paths is the repo-relative directories and files those units owned
	// (`removed-paths:`), slash-separated as they appear in the record.
	Paths map[string]bool
}

// Covers reports whether the tree deliberately dropped the repo-relative path.
// A directory covers everything beneath it, so `features/chat` covers
// `features/chat/handlers`.
func (p TrimProvenance) Covers(path string) bool {
	path = filepath.ToSlash(filepath.Clean(path))
	if p.Paths[path] {
		return true
	}
	for gone := range p.Paths {
		if strings.HasPrefix(path, gone+"/") {
			return true
		}
	}
	return false
}

// CoversAll reports whether the tree deliberately dropped EVERY directory the
// unit owns. One surviving directory means the slice is still here, so its card
// is still owed.
func (p TrimProvenance) CoversAll(dirs []string) bool {
	if len(dirs) == 0 {
		return false
	}
	for _, d := range dirs {
		if !p.Covers(d) {
			return false
		}
	}
	return true
}

// TrimmedPathSet is the record's paths as a plain set, for readers whose
// question is a map lookup (`cmd/check-docs` asks "is this token a path this
// tree deliberately deleted?"). It answers with the DIRECTORIES the record
// names, not their contents: a caller that needs prefix matching uses
// TrimProvenance.Covers instead.
func TrimmedPathSet(root string) map[string]bool {
	p := ReadTrimProvenance(root)
	out := make(map[string]bool, len(p.Paths))
	for path := range p.Paths {
		out[path] = true
	}
	return out
}

// ReadTrimProvenance reads the record from root's AGENTS.md. A missing file, a
// missing section, or an empty value all yield the zero value: strict checking,
// exactly as before the record existed.
func ReadTrimProvenance(root string) TrimProvenance {
	p := TrimProvenance{Removed: map[string]bool{}, Paths: map[string]bool{}}
	raw, err := os.ReadFile(filepath.Join(root, "AGENTS.md"))
	if err != nil {
		return p
	}
	return ParseTrimProvenance(string(raw))
}

// ParseTrimProvenance is the text half, split out so a test can drive it
// without a tree.
func ParseTrimProvenance(text string) TrimProvenance {
	p := TrimProvenance{Removed: map[string]bool{}, Paths: map[string]bool{}}
	section, ok := afterHeading(text, trimProvenanceHeading)
	if !ok {
		return p
	}
	p.Recorded = true
	for line := range strings.SplitSeq(section, "\n") {
		if rest, ok := strings.CutPrefix(strings.TrimSpace(line), "removed:"); ok {
			p.Removed = splitRecord(rest)
		}
		if rest, ok := strings.CutPrefix(strings.TrimSpace(line), "removed-paths:"); ok {
			p.Paths = splitRecord(rest)
		}
	}
	return p
}

// afterHeading returns the text below heading, up to the next `## ` heading.
func afterHeading(text, heading string) (string, bool) {
	var out strings.Builder
	found := false
	for line := range strings.SplitSeq(text, "\n") {
		trimmed := strings.TrimSpace(line)
		if !found {
			if trimmed == heading {
				found = true
			}
			continue
		}
		if strings.HasPrefix(trimmed, "## ") {
			break
		}
		out.WriteString(line)
		out.WriteString("\n")
	}
	return out.String(), found
}

// splitRecord reads one comma-separated record. `none` is the scaffold's word
// for an empty set, not a path.
func splitRecord(value string) map[string]bool {
	out := map[string]bool{}
	for item := range strings.SplitSeq(strings.TrimSpace(value), ",") {
		if item = strings.TrimSpace(item); item != "" && item != "none" {
			out[item] = true
		}
	}
	return out
}
