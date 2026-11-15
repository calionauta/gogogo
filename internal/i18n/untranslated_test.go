// The i18n ratchet: the untranslated debt is counted, and it can only shrink.
//
// WHY A RATCHET AND NOT A HARD GATE. Every check in this repo that says "no
// literal copy" is one-directional: it proves "every key you asked for exists",
// never "every English string was asked for". Twelve feature templates contain
// zero i18n calls and still pass, which is exactly the report that shipped
// (sidebar in pt-BR, every screen's body in English). A hard gate today would
// fail on ~190 pre-existing literals and would be disabled on day one.
//
// So the debt is a number in a checked-in file. The test fails when a file grows
// past its count; it never fails because a file is still above zero. Removing a
// literal means lowering the number, which is the migration.
//
// The scan is deliberately narrow: a text node that starts with an uppercase
// letter and holds at least 4 characters, excluding entities, code, paths, and
// the volatile bits a template interpolates. It is a heuristic, and it is
// documented as one — a missed literal costs a new English word, a false
// positive costs one allowlist line.
package i18n_test

import (
	"encoding/json"
	"flag"
	"io"
	"io/fs"
	"os"
	"path/filepath"
	"regexp"
	"sort"
	"strings"
	"testing"
)

// untranslatedBaseline is the ratchet file: path → number of literal text nodes.
const untranslatedBaseline = "testdata/untranslated.json"

// updateBaseline regenerates the ratchet from the current tree. It is the ONE
// way to accept a lower number, and it must be paired with the actual
// translation in the same commit.
var updateBaseline = flag.Bool("update-untranslated", false,
	"rewrite testdata/untranslated.json from the current tree")

// literalText matches a text node that reads as prose: it starts uppercase,
// has at least four letters, and contains no template expression or markup.
var literalText = regexp.MustCompile(`>([A-Z][A-Za-z][A-Za-z ,.'!?—–-]{2,})<`)

// notProse are the node contents that are data, not copy: an entity, a path, a
// URL, a bare acronym, or something a template interpolates.
var notProse = regexp.MustCompile(
	`^(&|https?:|/|[A-Z0-9]{1,3}$|[A-Za-z]+\(|.*\{.*\}.*$|[A-Za-z0-9_.-]+/[A-Za-z0-9_.-]+)`)

// scannedTrees are the trees whose markup must be translatable.
var scannedTrees = []string{"features", "internal"}

// countLiteralText returns the prose text nodes of one file.
func countLiteralText(src string) int {
	n := 0
	for line := range strings.SplitSeq(src, "\n") {
		trimmed := strings.TrimSpace(line)
		// A comment is documentation, not copy.
		if strings.HasPrefix(trimmed, "//") || strings.HasPrefix(trimmed, "{{/*") {
			continue
		}
		for _, m := range literalText.FindAllStringSubmatch(line, -1) {
			if notProse.MatchString(strings.TrimSpace(m[1])) {
				continue
			}
			n++
		}
	}
	return n
}

// probeUntranslated walks the templ files and returns count per repo path.
func probeUntranslated(t *testing.T) map[string]int {
	t.Helper()
	// Root-scoped reads: the walk visits paths this test does not control, and a
	// planted symlink would defeat a plain os.ReadFile (gosec G122). os.OpenRoot
	// refuses an escape at the kernel level.
	root, err := os.OpenRoot(sourceRoot)
	if err != nil {
		t.Fatalf("open root: %v", err)
	}
	defer func() { _ = root.Close() }()

	out := map[string]int{}
	for _, tree := range scannedTrees {
		walkErr := filepath.WalkDir(filepath.Join(sourceRoot, tree), func(path string, d fs.DirEntry, err error) error {
			if err != nil {
				return err
			}
			if d.IsDir() || !strings.HasSuffix(path, ".templ") {
				return nil
			}
			rel, relErr := filepath.Rel(sourceRoot, path)
			if relErr != nil {
				return relErr
			}
			f, openErr := root.Open(filepath.ToSlash(rel))
			if openErr != nil {
				return openErr
			}
			raw, readErr := io.ReadAll(f)
			_ = f.Close()
			if readErr != nil {
				return readErr
			}
			if n := countLiteralText(string(raw)); n > 0 {
				out[filepath.ToSlash(rel)] = n
			}
			return nil
		})
		if walkErr != nil {
			t.Fatalf("walk %s: %v", tree, walkErr)
		}
	}
	return out
}

// TestUntranslatedCopyDoesNotGrow is the ratchet. A file may still hold
// untranslated copy — the baseline records how much — but it may not hold MORE.
func TestUntranslatedCopyDoesNotGrow(t *testing.T) {
	current := probeUntranslated(t)

	if *updateBaseline {
		if err := os.MkdirAll(filepath.Dir(untranslatedBaseline), 0o755); err != nil {
			t.Fatal(err)
		}
		b, merr := json.MarshalIndent(current, "", "  ")
		if merr != nil {
			t.Fatal(merr)
		}
		if werr := os.WriteFile(untranslatedBaseline, append(b, '\n'), 0o600); werr != nil {
			t.Fatal(werr)
		}
		return
	}

	raw, err := os.ReadFile(untranslatedBaseline)
	if err != nil {
		t.Fatalf("read %s: %v (the baseline is checked in; regenerate with -update-untranslated)", untranslatedBaseline, err)
	}
	var baseline map[string]int
	if err := json.Unmarshal(raw, &baseline); err != nil {
		t.Fatalf("parse %s: %v", untranslatedBaseline, err)
	}

	var grew []string
	for path, n := range current {
		was, known := baseline[path]
		if !known {
			grew = append(grew, path+" (new file with "+itoa(n)+" literal text node(s))")
			continue
		}
		if n > was {
			grew = append(grew, path+" ("+itoa(was)+" → "+itoa(n)+")")
		}
	}
	sort.Strings(grew)
	for _, g := range grew {
		t.Errorf("untranslated copy grew: %s — wrap the new text in i18n.T(ctx, \"<feature>.<key>\", nil)"+
			" and add the key to every catalog", g)
	}
	if len(grew) > 0 {
		t.Log("if a count legitimately went down, regenerate with " +
			"-run TestUntranslatedCopyDoesNotGrow -update-untranslated")
	}
}

// itoa is a tiny helper so the failure message needs no fmt import.
func itoa(n int) string {
	if n == 0 {
		return "0"
	}
	var b []byte
	for n > 0 {
		b = append([]byte{byte('0' + n%10)}, b...)
		n /= 10
	}
	return string(b)
}
