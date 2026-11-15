// The i18n gate: no untranslated prose in markup.
//
// WHY THIS EXISTS. Every other check here is one-directional: it proves "every
// key you asked for exists", never "every English string was asked for". Twelve
// feature templates contained zero i18n calls and still passed — which is
// exactly the report that shipped (sidebar in pt-BR, every screen's body in
// English).
//
// It began as a ratchet because ~210 pre-existing literals would have failed a
// hard gate on day one. The debt is now ZERO (the checked-in file is `{}`), so
// this is a hard gate: any new literal prose node fails the build. Keep the file
// empty; the `-update-untranslated` flag exists only to re-measure a tree that
// legitimately shrank a count.
//
// The scan is deliberate about what it ignores — entities, code spans, paths,
// URLs, acronyms, env settings, template interpolations and product names — so
// the only thing that can trip it is copy a person would expect translated.
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
//
// MULTILINE ON PURPOSE. A wrapped sentence —
//
//	<p>Every note is shared: anyone signed in can open and edit it. Only its
//	author can delete it.</p>
//
// — is one text node split across lines by the formatter. A line-based scan saw
// neither half (the second line starts lowercase and the first has no closing
// ">"), so ~20 sentences looked translated when they were not. `(?s)` plus a
// non-greedy body over <> allows the newline.
var literalText = regexp.MustCompile(`(?s)>([A-Z][^<>{}]{3,}?)<`)

// notProse are the node contents that are data, not copy: an entity, a path, a
// URL, a bare acronym, an env-var setting, a proper noun, or something a
// template interpolates.
var notProse = regexp.MustCompile(
	`^(&|https?:|/|[A-Z0-9]{1,3}$|` +
		`[A-Z][A-Z0-9_]{2,}=|` + // RBAC_ENABLED=true — a setting, not a sentence
		`[A-Za-z]+\(|.*\{.*\}.*$|[A-Za-z0-9_.-]+/[A-Za-z0-9_.-]+)`)

// properNouns are product and protocol names that read as text but are never
// translated: they are the same word in every locale.
var properNouns = map[string]bool{
	"DagNats": true, "GoAkt": true, "Loro": true, "NATS": true, "JetStream": true,
	"Turso": true, "Templ": true, "Datastar": true, "Genkit": true, "GoAI": true,
	"Casbin": true, "DaisyUI": true, "PostHog": true, "Todo": true, "MCP": true,
	"GitHub": true, "PostgreSQL": true, "SQLite": true,
}

// scannedTrees are the trees whose markup must be translatable.
var scannedTrees = []string{"features", "internal"}

// countLiteralText returns the prose text nodes of one file.
func countLiteralText(src string) int {
	n := 0
	// Comments are documentation, not copy: strip them first so a sentence in a
	// Go/templ comment is not counted as shippable prose.
	cleaned := regexp.MustCompile(`(?m)^\s*(//.*|\{\{/\*.*?\*/\}\})\s*$`).ReplaceAllString(src, "")
	for _, m := range literalText.FindAllStringSubmatch(cleaned, -1) {
		body := strings.TrimSpace(m[1])
		body = strings.Join(strings.Fields(body), " ")
		if notProse.MatchString(body) || properNouns[body] {
			continue
		}
		n++
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
