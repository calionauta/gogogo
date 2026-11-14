// Command check-prompts is the deterministic half of the prompt-as-data rule.
//
// Two invariants, both mechanical:
//
//  1. Every .prompt file in internal/prompts/files is a valid Dotprompt file:
//     YAML frontmatter with a name matching the path, a description, a version,
//     and a declared variable list that matches the body exactly.
//  2. No prompt lives in Go. A long string literal that reads like model
//     instructions ("you are", "return only", "reply with", ...) fails the gate,
//     so the only way to add a prompt is a file in the library.
//
// The heuristic is deliberately conservative: it needs a long literal, several
// distinct instruction markers and real sentence structure. A code template or
// an error message cannot reach the threshold by accident, and a genuine
// prompt cannot stay under it. A deliberate exception carries a reason:
//
//	// allow-inline-prompt: short greeting, not a model instruction
//
// The escape hatch exists because a gate with no exception path gets disabled
// the first time it is wrong; review reads the reasons.
package main

import (
	"flag"
	"fmt"
	"go/ast"
	"go/parser"
	"go/token"
	"os"
	"path/filepath"
	"strconv"
	"strings"
	"unicode/utf8"

	"github.com/calionauta/gogogo/internal/prompts"
)

// scanRoots are the trees where a prompt could be smuggled into Go. The
// installer is included: its AGENTS.md template is model-facing text and now
// lives in the library too.
var scanRoots = []string{"cmd", "config", "features", "internal"}

// skipDirs are generated or vendored trees, plus the prompt library itself
// (its loader mentions instruction words in doc comments, not literals).
var skipDirs = map[string]bool{
	".git": true, "node_modules": true, "tmp": true, "build": true, "data": true,
	"vendor": true,
}

// instructionMarkers are phrases that appear in model-facing text and almost
// never in an error message, a log line or a code template. The gate needs
// minMarkers distinct ones.
var instructionMarkers = []string{
	"you are", "return only", "reply with", "answer with", "respond",
	"do not", "never ", "must ", "exactly:", "the user", "suggest ",
	"instructions", "prompt",
}

const (
	minLiteralRunes = 120
	minMarkers      = 3
	minSentences    = 2
	exemptPrefix    = "allow-inline-prompt:"
)

func main() {
	quiet := flag.Bool("quiet", false, "print only failures")
	flag.Parse()

	var problems []string
	files, err := prompts.All()
	if err != nil {
		problems = append(problems, "prompt library: "+err.Error())
	}
	if len(files) == 0 && err == nil {
		problems = append(problems, "prompt library: no .prompt files found")
	}
	inline, scanErrs := scanInlinePrompts(".")
	problems = append(problems, scanErrs...)
	for _, hit := range inline {
		problems = append(problems, hit.String())
	}
	declared, declaredErrs := scanDeclaredPrompts(".")
	problems = append(problems, declaredErrs...)
	for _, hit := range declared {
		problems = append(problems, hit.String())
	}
	unused, unusedErrs := scanUnusedPrompts(".")
	problems = append(problems, unusedErrs...)
	for _, hit := range unused {
		problems = append(problems, hit.String())
	}

	// A candidate tree (the optimizer's seam) is gated too, against the library.
	overrideChecked := 0
	if dir := os.Getenv(prompts.OverrideDirEnv); dir != "" {
		overProblems, n := checkOverrideTree(dir)
		problems = append(problems, overProblems...)
		overrideChecked = n
	}

	if len(problems) > 0 {
		for _, p := range problems {
			fmt.Fprintln(os.Stderr, p)
		}
		fmt.Fprintf(os.Stderr,
			"check-prompts: %d problem(s); model-facing text belongs in "+
				"internal/prompts/files/<domain>/<name>.prompt\n", len(problems))
		os.Exit(1)
	}
	if !*quiet {
		fmt.Printf("check-prompts: %d prompt file(s) valid, no inline prompts in Go\n", len(files))
		if overrideChecked > 0 {
			fmt.Printf("check-prompts: %d override file(s) valid against the library\n", overrideChecked)
		}
	}
}

// inlineHit is one prompt-shaped literal found in Go source.
type inlineHit struct {
	Path string
	Line int
	Runs int
	Text string
}

// declaredHit is a model-facing prompt declared as a Go literal. The length
// heuristic above cannot see a short prompt, but a declared prompt has a
// field name: `PanelSpec{Prompt: "..."}` is model-facing by construction, so
// the field name is the rule and the length never matters.
type declaredHit struct {
	Path string
	Line int
	Text string
}

func (h declaredHit) String() string {
	return fmt.Sprintf("%s:%d: model-facing prompt declared inline (%q) — load it from internal/prompts/files",
		h.Path, h.Line, h.Text)
}

// unusedFile is a prompt file no code references. A file nobody loads is a
// second copy of a prompt that will drift, or a prompt that was migrated and
// left behind — the residue this gate exists to catch.
type unusedFile struct {
	ID   string
	Path string
}

func (h unusedFile) String() string {
	return fmt.Sprintf("%s: prompt file %q is referenced by no code — a dead prompt is a second copy waiting to drift",
		h.Path, h.ID)
}

func (h inlineHit) String() string {
	const excerptLimit = 60
	excerpt := []rune(h.Text)
	if len(excerpt) > excerptLimit {
		excerpt = append(excerpt[:excerptLimit-3], '.', '.', '.')
	}
	return fmt.Sprintf("%s:%d: prompt-shaped string literal (%d chars) — move it to internal/prompts/files: %q",
		h.Path, h.Line, h.Runs, string(excerpt))
}

// scanDeclaredPrompts walks the repo and reports model-facing prompts
// declared as Go literals. The length heuristic cannot see a short prompt, but
// a declared prompt has a field name: `PanelSpec{Prompt: "..."}` is model-facing
// by construction, so the field name is the rule and the length never matters.
func scanDeclaredPrompts(root string) ([]declaredHit, []string) {
	files, errs := goFiles(root)
	var hits []declaredHit
	for _, path := range files {
		fileHits, fileErrs := declaredInFile(path)
		hits = append(hits, fileHits...)
		errs = append(errs, fileErrs...)
	}
	return hits, errs
}

// declaredInFile reports the inline prompt declarations in one Go file.
func declaredInFile(path string) ([]declaredHit, []string) {
	src, err := os.ReadFile(path)
	if err != nil {
		return nil, []string{"check-prompts: " + err.Error()}
	}
	if strings.Contains(string(src), "Code generated by") {
		return nil, nil
	}
	fset := token.NewFileSet()
	file, err := parser.ParseFile(fset, path, src, parser.SkipObjectResolution)
	if err != nil {
		return nil, []string{"check-prompts: " + path + ": " + err.Error()}
	}
	var hits []declaredHit
	ast.Inspect(file, func(n ast.Node) bool {
		lit, ok := n.(*ast.CompositeLit)
		if !ok {
			return true
		}
		for _, elt := range lit.Elts {
			kv, isKV := elt.(*ast.KeyValueExpr)
			if !isKV {
				continue
			}
			key, isIdent := kv.Key.(*ast.Ident)
			if !isIdent || key.Name != "Prompt" {
				continue
			}
			val, isLit := kv.Value.(*ast.BasicLit)
			if !isLit || val.Kind != token.STRING {
				continue
			}
			text, unqErr := strconv.Unquote(val.Value)
			if unqErr != nil {
				continue
			}
			hits = append(hits, declaredHit{
				Path: path,
				Line: fset.Position(val.Pos()).Line,
				Text: text,
			})
		}
		return true
	})
	return hits, nil
}

// scanUnusedPrompts reports prompt files no code loads. A file nobody reads is
// a second copy of a prompt that will drift, or the residue of a migration
// that left the old string in the code.
func scanUnusedPrompts(root string) ([]unusedFile, []string) {
	all, err := prompts.All()
	if err != nil {
		return nil, []string{"check-prompts: " + err.Error()}
	}
	code, errs := goSources(root)
	var unused []unusedFile
	for _, id := range unusedPromptIDs(code, all) {
		path := filepath.Join(root, prompts.Dir, id+".prompt")
		unused = append(unused, unusedFile{ID: id, Path: path})
	}
	return unused, errs
}

// unusedPromptIDs returns the ids no code references. The id must appear as a
// quoted string, so a longer id that starts with it (a renamed file) does not
// count as a reference.
func unusedPromptIDs(code string, all []prompts.Prompt) []string {
	var unused []string
	for _, p := range all {
		if !strings.Contains(code, "\""+p.ID+"\"") {
			unused = append(unused, p.ID)
		}
	}
	return unused
}

// goSources concatenates every non-test Go file outside the prompt library, so
// a prompt id can be searched for in one pass.
func goSources(root string) (string, []string) {
	files, errs := goFiles(root)
	var b strings.Builder
	for _, path := range files {
		raw, readErr := os.ReadFile(path)
		if readErr != nil {
			errs = append(errs, "check-prompts: "+readErr.Error())
			continue
		}
		b.Write(raw)
		b.WriteByte('\n')
	}
	return b.String(), errs
}

// goFiles lists the non-test Go files the scanners read, outside the prompt
// library itself. One walker for every scanner: three copies of the same walk
// is three places to keep in step, and the gate is the last line of defence.
func goFiles(root string) ([]string, []string) {
	var files []string
	var errs []string
	for _, top := range scanRoots {
		walkErr := filepath.WalkDir(filepath.Join(root, top), func(path string, d os.DirEntry, err error) error {
			if err != nil {
				return err
			}
			if d.IsDir() {
				if skipDirs[d.Name()] || path == filepath.Join(root, "internal", "prompts") {
					return filepath.SkipDir
				}
				return nil
			}
			if strings.HasSuffix(path, ".go") && !strings.HasSuffix(path, "_test.go") {
				files = append(files, path)
			}
			return nil
		})
		if walkErr != nil {
			errs = append(errs, "check-prompts: "+walkErr.Error())
		}
	}
	return files, errs
}

// scanInlinePrompts reports prompt-shaped Go literals.
func scanInlinePrompts(root string) ([]inlineHit, []string) {
	files, errs := goFiles(root)
	var hits []inlineHit
	for _, path := range files {
		fileHits, fileErrs := scanFile(path)
		hits = append(hits, fileHits...)
		errs = append(errs, fileErrs...)
	}
	return hits, errs
}

// scanFile reports prompt-shaped literals in one Go file.
func scanFile(path string) ([]inlineHit, []string) {
	src, err := os.ReadFile(path)
	if err != nil {
		return nil, []string{"check-prompts: " + err.Error()}
	}
	if strings.Contains(string(src), "Code generated by") {
		return nil, nil
	}
	fset := token.NewFileSet()
	file, err := parser.ParseFile(fset, path, src, parser.SkipObjectResolution)
	if err != nil {
		return nil, []string{"check-prompts: " + path + ": " + err.Error()}
	}
	lines := strings.Split(string(src), "\n")
	var hits []inlineHit
	ast.Inspect(file, func(n ast.Node) bool {
		switch expr := n.(type) {
		case *ast.BinaryExpr:
			if text, ok := stringConcat(expr); ok {
				if hit, found := evaluate(path, fset, lines, expr.Pos(), text); found {
					hits = append(hits, hit)
				}
				return false // the whole concatenation is checked once
			}
		case *ast.BasicLit:
			if expr.Kind != token.STRING {
				return true
			}
			if text, err := strconv.Unquote(expr.Value); err == nil {
				if hit, found := evaluate(path, fset, lines, expr.Pos(), text); found {
					hits = append(hits, hit)
				}
			}
		}
		return true
	})
	return hits, nil
}

// stringConcat flattens a `+` chain made only of string literals. Anything else
// (a variable, a call) is not a literal prompt and is left alone.
func stringConcat(expr ast.Expr) (string, bool) {
	switch e := expr.(type) {
	case *ast.BasicLit:
		if e.Kind != token.STRING {
			return "", false
		}
		text, err := strconv.Unquote(e.Value)
		return text, err == nil
	case *ast.BinaryExpr:
		if e.Op != token.ADD {
			return "", false
		}
		left, lok := stringConcat(e.X)
		right, rok := stringConcat(e.Y)
		if !lok || !rok {
			return "", false
		}
		return left + right, true
	}
	return "", false
}

// evaluate applies the heuristic and the exemption marker.
func evaluate(path string, fset *token.FileSet, lines []string, pos token.Pos, text string) (inlineHit, bool) {
	if utf8.RuneCountInString(text) < minLiteralRunes {
		return inlineHit{}, false
	}
	if !looksLikePrompt(text) {
		return inlineHit{}, false
	}
	line := fset.Position(pos).Line
	if hasExemption(lines, line) {
		return inlineHit{}, false
	}
	return inlineHit{Path: path, Line: line, Runs: utf8.RuneCountInString(text), Text: text}, true
}

// looksLikePrompt is the shared heuristic: enough instruction vocabulary and
// real sentences that this reads as something a model is told.
func looksLikePrompt(text string) bool {
	lower := strings.ToLower(text)
	markers := 0
	for _, marker := range instructionMarkers {
		if strings.Contains(lower, marker) {
			markers++
		}
	}
	if markers < minMarkers {
		return false
	}
	sentences := 0
	for _, stop := range []string{". ", ".\n", "? ", "! "} {
		sentences += strings.Count(text, stop)
	}
	return sentences >= minSentences
}

// hasExemption reports whether the literal's line, or the line immediately
// above it, carries a non-empty `allow-inline-prompt:` reason.
func hasExemption(lines []string, line int) bool {
	for _, n := range []int{line, line - 1} {
		if n-1 < 0 || n-1 >= len(lines) {
			continue
		}
		if idx := strings.Index(lines[n-1], exemptPrefix); idx >= 0 {
			if strings.TrimSpace(lines[n-1][idx+len(exemptPrefix):]) != "" {
				return true
			}
		}
	}
	return false
}
