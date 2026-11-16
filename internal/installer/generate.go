// doctor/generate preflight+codegen (see docs/doctor-generate.md)
// generate - deterministic codegen wrapper (see docs/doctor-generate.md)
//
//nolint:lll,goconst,mnd,gocyclo,funlen,gosec,staticcheck,contextcheck,noctx,nolintlint,perfsprint // see docs/doctor-generate.md; CI gates are --json + lefthook + generate --check
package installer

//nolint:lll,goconst,mnd,gocyclo,funlen,gosec,staticcheck,contextcheck,noctx,staticcheck,goconst,lll,mnd,gocyclo,funlen,gosec,contextcheck,noctx,nolintlint,perfsprint // see docs/doctor-generate.md; CI gates are --json + lefthook + generate --check

import (
	"context"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"flag"
	"fmt"
	"io"
	"os"
	"os/exec"
	"path/filepath"
	"sort"
	"strings"
)

const (
	generateTail30 = 30
	generateTail40 = 40
)

// generateOutputs are the paths the steps above rewrite. `--check` scopes to
// them: a gate that reacts to a README edit is a gate nobody can run.
//
// "**/*_templ.go" is a recursive pattern, not a glob and not a shell: templ's
// output lives BESIDE every .templ, and this tree has none at the root — a
// plain "*_templ.go" matched zero files and silently exempted the only artifact
// `templ generate` produces.
var generateOutputs = []string{
	"internal/db/sqlcgen",
	"web/resources/static/app.min.css",
	"docs/api-routes.md",
	"docs/unit-contracts.md",
	recursiveTemplPattern,
}

// recursiveTemplPattern names every generated templ file, wherever the .templ
// lives. Written once so the snapshot and the check cannot disagree.
const recursiveTemplPattern = "**/*_templ.go"

func marshalJSON(v any) ([]byte, error) { return json.MarshalIndent(v, "", "  ") }

// changedOutputs lists the owned files whose bytes the run changed.
func changedOutputs(before map[string]string, patterns []string) ([]string, error) {
	var dirty []string
	for _, pattern := range patterns {
		files, err := expandPattern(pattern)
		if err != nil {
			return nil, err
		}
		for _, path := range files {
			now, rerr := fileHash(path)
			if rerr != nil {
				return nil, rerr
			}
			if was, ok := before[path]; ok && was != now {
				dirty = append(dirty, path)
			}
		}
	}
	sort.Strings(dirty)
	return dirty, nil
}

// snapshotOutputs records the bytes of every owned file before the run.
func snapshotOutputs(patterns []string) (map[string]string, error) {
	out := map[string]string{}
	for _, pattern := range patterns {
		files, err := expandPattern(pattern)
		if err != nil {
			return nil, err
		}
		for _, path := range files {
			sum, rerr := fileHash(path)
			if rerr != nil {
				return nil, rerr
			}
			out[path] = sum
		}
	}
	return out, nil
}

// expandPattern resolves one owned path: a recursive pattern walks the tree, a
// FILE is itself, a DIRECTORY is every Go file under it (the generated output a
// step rewrites).
func expandPattern(pattern string) ([]string, error) {
	if recursive, ok := strings.CutPrefix(pattern, "**/"); ok {
		// The remainder is a glob TAIL ("*_templ.go"); drop the wildcard, since
		// the match is a suffix test. Keeping it compared a literal asterisk and
		// matched nothing — silently exempting every generated templ file.
		return expandRecursive(strings.TrimPrefix(recursive, "*"))
	}
	info, err := os.Stat(pattern)
	if err == nil && !info.IsDir() {
		return []string{pattern}, nil
	}
	if err != nil && !strings.Contains(pattern, "*") {
		return nil, nil // the step did not produce it (a tree without sqlc)
	}
	matches, gerr := filepath.Glob(pattern)
	if gerr != nil {
		return nil, gerr
	}
	var out []string
	for _, match := range matches {
		entries, rerr := os.ReadDir(match)
		if rerr != nil {
			out = append(out, match)
			continue
		}
		for _, e := range entries {
			if !e.IsDir() && strings.HasSuffix(e.Name(), ".go") {
				out = append(out, filepath.Join(match, e.Name()))
			}
		}
	}
	return out, nil
}

// expandRecursive returns every file whose name matches suffix, at any depth.
// It skips the same noise every tree scan in this package skips.
func expandRecursive(suffix string) ([]string, error) {
	var out []string
	err := filepath.WalkDir(".", func(path string, d os.DirEntry, walkErr error) error {
		if walkErr != nil {
			return walkErr
		}
		if d.IsDir() {
			if skipDriftDir(d.Name()) {
				return filepath.SkipDir
			}
			return nil
		}
		if strings.HasSuffix(d.Name(), suffix) {
			out = append(out, filepath.ToSlash(path))
		}
		return nil
	})
	if err != nil {
		return nil, err
	}
	sort.Strings(out)
	return out, nil
}

func fileHash(path string) (string, error) {
	raw, err := os.ReadFile(path)
	if err != nil {
		return "", err
	}
	sum := sha256.Sum256(raw)
	return hex.EncodeToString(sum[:]), nil
}

func runGenerate(ctx context.Context, args []string, stdout io.Writer) error {
	fs := flag.NewFlagSet("generate", flag.ContinueOnError)
	jsonOut := fs.Bool("json", false, "machine-readable")
	check := fs.Bool("check", false, "fail on drift (CI)")
	only := fs.String("only", "", "templ|sqlc|css|docs (comma-separated)")
	fs.Usage = func() { PrintUsage(stdout, fs) }
	if err := fs.Parse(args); err != nil {
		return err
	}
	_ = exec.CommandContext(ctx, "true") // keep ctx used
	before, snapErr := snapshotOutputs(generateOutputs)
	if snapErr != nil {
		return &ExitError{code: 2, msg: "generate: " + snapErr.Error()}
	}
	steps := []string{"templ", "sqlc", "css", "docs"}
	var ran []string
	runOnly := map[string]bool{}
	if *only != "" {
		for s := range strings.SplitSeq(*only, ",") {
			runOnly[strings.TrimSpace(s)] = true
		}
	}

	// 1. templ generate (idempotent)
	if runOnlyTrue(runOnly, "templ") {
		if ok, err := runTemplGenerateRepo(ctx); err != nil {
			return &ExitError{code: 2, msg: fmt.Sprintf("templ generate failed: %v", err)}
		} else if ok {
			ran = append(ran, "templ")
		}
	}
	// 2. sqlc generate (if installed + config present)
	if runOnlyTrue(runOnly, "sqlc") {
		if ok, err := runSQLCGenerateRepo(ctx); err != nil {
			return &ExitError{code: 2, msg: fmt.Sprintf("sqlc generate failed: %v", err)}
		} else if ok {
			ran = append(ran, "sqlc")
		}
	}
	// 3. css (make css — tailwind v4 + daisyui)
	if runOnlyTrue(runOnly, "css") {
		if out, err := runIn(ctx, ".", "make", "css"); err != nil {
			return &ExitError{code: 2, msg: fmt.Sprintf("make css failed: %v\n%s", err, tailLines(string(out), generateTail30))}
		}
		ran = append(ran, "css")
	}
	// 4. docs regen
	if runOnlyTrue(runOnly, "docs") {
		if out, err := runIn(ctx, ".", "go", "test", "./internal/capabilities/", "-run", "Test(ApiRoutes|CapabilityContracts)DocFresh", "-update", "-count=1"); err != nil {
			return &ExitError{code: 2, msg: fmt.Sprintf("docs regen failed: %v\n%s", err, tailLines(string(out), generateTail30))}
		}
		ran = append(ran, "docs")
	}
	// --check asks ONE question: did generation change the files it owns?
	//
	// Not `git diff --exit-code` (with or without paths): that compares the
	// working tree against HEAD, so a branch with any unrelated edit — the
	// normal state while working — reported "drift", and a committed tree whose
	// generator was already applied looked clean for the wrong reason. Hashing
	// before and after answers the real question and needs no git.
	if *check {
		dirty, derr := changedOutputs(before, generateOutputs)
		if derr != nil {
			return &ExitError{code: 2, msg: "generate --check: " + derr.Error()}
		}
		if len(dirty) > 0 {
			return &ExitError{code: 1, msg: "generate --check: generation changed " +
				strings.Join(dirty, ", ") + "\n  commit the regenerated files (they are checked in)"}
		}
	}
	if *jsonOut {
		fmt.Fprintf(stdout, "{\"ran\":%q}\n", strings.Join(ran, ","))
	} else {
		fmt.Fprintf(stdout, "gogogo generate: ran %v\n", ran)
	}
	_ = steps
	return nil
}

func runOnlyTrue(only map[string]bool, key string) bool {
	if len(only) == 0 {
		return true
	}
	return only[key]
}

func runTemplGenerateRepo(_ context.Context) (bool, error) {
	return runTemplGenerate(".") // reuse recipe_codegen seam (which uses go tool templ)
}

func runSQLCGenerateRepo(_ context.Context) (bool, error) {
	return runSQLCGenerate(".")
}
