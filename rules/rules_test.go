package gorules

import (
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"testing"
)

// These tests run `golangci-lint` against a throwaway module holding a
// deliberately bad fixture, and assert the expected rule fires — and that the
// corrected code stays quiet.
//
// WHY THIS EXISTS. A ruleguard rule that matches nothing is indistinguishable
// from one that is broken, and a broken rule FAILS OPEN: CI stays green and the
// footgun ships. That is not hypothetical — while writing
// TickerLoopWithoutExit, a single-arm version looked obviously correct and
// silently missed half the cases, and two separate "the rule never fires"
// conclusions turned out to be a broken environment, not a rule. Nothing in
// this directory was machine-checked before these tests.
//
// WHY NOT go-ruleguard's own test API. The engine
// (`github.com/quasilyte/go-ruleguard`) is NOT a dependency of this module —
// only its `dsl` subpackage is, because that is what rules.go imports. Adding
// the engine would be a new dependency just for a test, and this repo's rule is
// to reach for what exists first. `golangci-lint` is already the runner CI and
// the pre-commit hook use, so this tests the REAL integration (config
// resolution, gocritic wiring, the load path) instead of a parallel harness that
// could disagree with it.
//
// THE FIXTURE MUST BE A REAL MODULE. Two non-obvious requirements, each of
// which produced a misleading "empty rule set" while this test was written:
//
//  1. It must require `github.com/quasilyte/go-ruleguard/dsl`, because
//     rules.go imports it. Without the dependency, ruleguard cannot compile the
//     rule file, load fails, and the only symptom is a generic
//     `used Run() with an empty rule set` error — which reads as "the rule is
//     wrong" rather than "the fixture cannot load it".
//  2. The `rules:` path is resolved against the LINTED DIRECTORY, not the
//     config file's directory. An absolute path pointing outside the module
//     fails the same way. So the fixture copies `rules.go` into itself and
//     refers to it relatively, which is also what the real `.golangci.yml` does.
//
// Fixtures live in a temp dir, not under `tmp/`: golangci-lint walks from the
// working directory, and a stray `package fixture` file inside this package
// would break the build.
//
// A missing `golangci-lint` SKIPS rather than fails, so `go test ./...` stays
// usable where only the compiled binary runs. The rule is still guarded where
// it matters: CI installs golangci-lint, and so does the pre-commit hook.

// ruleFixture is one bad-code sample plus what the linter must report about it.
type ruleFixture struct {
	name string
	// source is the fixture's Go file (package fixture).
	source string
	// wantAtLeast is the minimum number of ruleguard diagnostics. Asserting a
	// minimum rather than an exact count is deliberate — see the enumeration
	// note on TestRulesFire.
	wantAtLeast int
}

func goFile(body string) string {
	return "package fixture\n\nimport \"time\"\n\nvar _ = time.Second\n\n" + body + "\n"
}

func fixtures() []ruleFixture {
	all := tickerFixtures()
	all = append(all,
		ruleFixture{
			name: "the corrected select shape stays quiet",
			source: goFile(`func corrected(done <-chan struct{}, f func()) {
	t := time.NewTicker(time.Second)
	defer t.Stop()
	for {
		select {
		case <-done:
			return
		case <-t.C:
			f()
		}
	}
}`),
			wantAtLeast: 0,
		},
		ruleFixture{
			name: "a plain channel range stays quiet",
			source: goFile(`func plainChannel(f func()) {
	ch := make(chan int)
	for range ch {
		f()
	}
}`),
			wantAtLeast: 0,
		},
		ruleFixture{
			name: "time.After in a production select is flagged",
			source: goFile(`func loop(ctxDone <-chan struct{}) {
	for {
		select {
		case <-ctxDone:
			return
		case <-time.After(time.Second):
		}
	}
}`),
			wantAtLeast: 1,
		},
	)
	return all
}

// tickerFixtures are the TickerLoopWithoutExit cases, split out so each function
// stays inside the 100-line `funlen` budget this repo enforces.
func tickerFixtures() []ruleFixture {
	return []ruleFixture{
		{
			name: "identifier receiver is flagged",
			source: goFile(`func identReceiver(f func()) {
	t := time.NewTicker(time.Second)
	defer t.Stop()
	for range t.C {
		f()
	}
}`),
			wantAtLeast: 1,
		},
		{
			// The case a single-arm rule silently missed. Kept separate so a
			// regression names which receiver shape broke.
			name: "inline call receiver is flagged",
			source: goFile(`func callReceiver(f func()) {
	for range time.NewTicker(time.Second).C {
		f()
	}
}`),
			wantAtLeast: 1,
		},
		{
			// Both receiver shapes present at once, call-chain first: the
			// arrangement that exposed the missing half of the original rule.
			name: "both receiver shapes are flagged",
			source: goFile(`func first(f func()) {
	for range time.NewTicker(time.Second).C {
		f()
	}
}

func second(f func()) {
	t := time.NewTicker(time.Second)
	defer t.Stop()
	for range t.C {
		f()
	}
}`),
			wantAtLeast: 1,
		},
		{
			name: "repeated leaks in one file are all flagged",
			source: goFile(`func a(f func()) {
	t := time.NewTicker(time.Second)
	defer t.Stop()
	for range t.C {
		f()
	}
}

func b(f func()) {
	t := time.NewTicker(time.Second)
	defer t.Stop()
	for range t.C {
		f()
	}
}

func c(f func()) {
	t := time.NewTicker(time.Second)
	defer t.Stop()
	for range t.C {
		f()
	}
}`),
			// Three IDENTICAL loops are enumerated in full; this is the case
			// that proves the multi-hit path works at all.
			wantAtLeast: 3,
		},
	}
}

// TestRulesFire runs each fixture through the real linter and checks the rule
// fires (or stays quiet).
//
// ON ASSERTING A MINIMUM, NOT A COUNT. golangci-lint does not always enumerate
// every diagnostic the ruleguard engine produced. Measured on the two ticker
// shapes: one shape alone reports 1, three identical loops report all 3, but a
// call-chain loop FOLLOWED BY an identifier loop reports 1 of 2 while the
// reverse order reports both. The ruleguard engine itself reports all of them
// (checked by running `ruleguard` directly), so this is a limitation of the
// golangci-lint issue pipeline, not of the rules.
//
// What matters for enforcement is unaffected: a file containing any leak still
// fails the linter (exit 1). So the guard asserts the guarantee that holds —
// at least one hit for a leak, exactly zero for clean code — and the
// "repeated leaks" fixture locks in the case that proves full enumeration is
// possible. An exact-count assertion for the mixed file would be asserting a
// golangci-lint bug.
func TestRulesFire(t *testing.T) {
	bin, err := exec.LookPath("golangci-lint")
	if err != nil {
		t.Skip("golangci-lint not on PATH; skipping the ruleguard guard " +
			"(CI and the pre-commit hook run it with the binary installed)")
	}

	rulesSrc, err := os.ReadFile("rules.go")
	if err != nil {
		t.Fatal(err)
	}
	cfgSrc, err := os.ReadFile(filepath.Join("..", ".golangci.yml"))
	if err != nil {
		t.Fatal(err)
	}
	dslVersion := requireVersion(t, cfgSrc)

	for _, tc := range fixtures() {
		t.Run(tc.name, func(t *testing.T) {
			dir := t.TempDir()
			writeFixtureModule(t, dir, rulesSrc, dslVersion, tc.source)

			out := runLint(t, bin, dir)
			// A load failure also produces a "ruleguard:" line, so a zero could
			// hide one. Check it first, loudly.
			if strings.Contains(out, "empty rule set") {
				t.Fatalf("ruleguard could not LOAD the rules (fixture problem, "+
					"not a rule verdict):\n%s", out)
			}
			// A typecheck error in the fixture suppresses EVERY rule result and
			// reads as zero hits, so surface it rather than reporting a miss.
			if strings.Contains(out, "typecheck") {
				t.Fatalf("fixture does not compile, so no rule could fire:\n%s", out)
			}

			got := strings.Count(out, "ruleguard:")
			if got < tc.wantAtLeast {
				t.Fatalf("ruleguard hits = %d, want at least %d\nlinter output:\n%s",
					got, tc.wantAtLeast, out)
			}
			if tc.wantAtLeast == 0 && got != 0 {
				t.Fatalf("expected no diagnostics on clean code, got %d\n%s", got, out)
			}
		})
	}
}

// TestRulesAreWiredIntoTheProjectConfig is the companion guard: the rules in
// this file protect nothing if `.golangci.yml` stops pointing gocritic at them,
// and every fixture test above would still pass.
func TestRulesAreWiredIntoTheProjectConfig(t *testing.T) {
	raw, err := os.ReadFile(filepath.Join("..", ".golangci.yml"))
	if err != nil {
		t.Fatal(err)
	}
	cfg := string(raw)
	for _, want := range []string{"ruleguard", "rules/rules.go", "gocritic"} {
		if !strings.Contains(cfg, want) {
			t.Errorf(".golangci.yml no longer mentions %q — the rules in this "+
				"package would not run in CI", want)
		}
	}
}

// requireVersion extracts the go-ruleguard/dsl version the project pins, so the
// fixture requires the SAME one. Hardcoding it here would drift.
func requireVersion(t *testing.T, cfg []byte) string {
	t.Helper()
	mod, err := os.ReadFile(filepath.Join("..", "go.mod"))
	if err != nil {
		t.Fatal(err)
	}
	const prefix = "github.com/quasilyte/go-ruleguard/dsl v"
	for line := range strings.SplitSeq(string(mod), "\n") {
		line = strings.TrimSpace(line)
		if strings.HasPrefix(line, prefix) {
			return strings.TrimPrefix(line, "github.com/quasilyte/go-ruleguard/dsl ")
		}
	}
	t.Fatalf("go.mod no longer requires go-ruleguard/dsl — the rules file "+
		"imports it, so it cannot be dropped without breaking ruleguard.\n%s", cfg)
	return ""
}

// writeFixtureModule lays out a minimal module that can actually LOAD rules.go:
// the rules file copied in, the dsl dependency it imports, and a config pointing
// gocritic at it by a path relative to the linted directory.
func writeFixtureModule(t *testing.T, dir string, rulesSrc []byte, dslVersion, source string) {
	t.Helper()

	if err := os.MkdirAll(filepath.Join(dir, "rules"), 0o750); err != nil {
		t.Fatal(err)
	}
	writeIn(t, filepath.Join(dir, "go.mod"),
		"module fixture\n\ngo 1.22\n\nrequire github.com/quasilyte/go-ruleguard/dsl "+dslVersion+"\n")
	writeIn(t, filepath.Join(dir, "fixture.go"), source)
	writeIn(t, filepath.Join(dir, "rules", "rules.go"), string(rulesSrc))

	// Mirrors the project's own ruleguard block, minus every other linter, so a
	// fixture's diagnostic count has exactly one source.
	writeIn(t, filepath.Join(dir, "golangci.yml"), `version: "2"
linters:
  enable: [gocritic]
  settings:
    gocritic:
      enabled-checks: [ruleguard]
      settings:
        ruleguard:
          rules: "rules/rules.go"
  exclusions:
    generated: lax
    presets: [comments, common-false-positives, legacy, std-error-handling]
`)
}

func writeIn(t *testing.T, path, content string) {
	t.Helper()
	//nolint:gosec // G703: path is built from t.TempDir() plus literals in this
	// file; t.TempDir is per-test and removed by the testing package. Same
	// reasoning as scripts/agehelper/main.go, which uses the same directive.
	if err := os.WriteFile(path, []byte(content), 0o600); err != nil {
		t.Fatal(err)
	}
}

// runLint lints the fixture module.
//
// Three non-obvious requirements, each of which produced a silent zero hits
// while this test was written (a broken harness is indistinguishable from a
// rule that never fires):
//
//  1. GOLANGCI_LINT_CACHE must point at a FRESH, ISOLATED directory (below).
//     Rules are compiled and cached by path, and a stale entry for the same
//     filename gets served — including a broken rule, which is how a red-proof
//     here "passed" until the cache was cleared by hand.
//     NOT `golangci-lint cache clean`: that is forbidden on the shared host this
//     repo is developed on, because it destroys the cache every other run
//     depends on for speed. An isolated cache per run is strictly better here:
//     it is empty by construction, so there is nothing stale to invalidate.
//  2. `--config <abs path>` is REQUIRED. With auto-discovery the ruleguard
//     plugin does not compile, and the only symptom is zero diagnostics.
//  3. `--allow-parallel-runners` is required, because golangci-lint holds a
//     global /tmp/golangci-lint.lock and otherwise refuses to start while
//     another instance runs (a leftover lock from a killed run is enough).
//  4. max-same-issues/max-issues-per-linter are disabled, since the defaults
//     (3/50) collapse identical diagnostics.
func runLint(t *testing.T, bin, dir string) string {
	t.Helper()
	//nolint:gosec // G204: bin is exec.LookPath("golangci-lint"); every argument
	// is a literal or a path this test just created under t.TempDir().
	cmd := exec.CommandContext(t.Context(), bin, "run",
		"--config", filepath.Join(dir, "golangci.yml"),
		"--allow-parallel-runners",
		"--max-same-issues=0", "--max-issues-per-linter=0", "./...")
	cmd.Dir = dir
	// -mod=mod so the fixture's go.sum-less module resolves from the local
	// module cache instead of failing a -mod=readonly check. The isolated lint
	// cache lives inside the fixture, which t.TempDir removes afterwards.
	// Built in one expression: gocritic's appendAssign objects to appending onto
	// a variable that was itself produced by a different call.
	cmd.Env = append(
		withoutEnv(withoutEnv(os.Environ(), "GOFLAGS"), "GOLANGCI_LINT_CACHE"),
		"GOFLAGS=-mod=mod",
		"GOLANGCI_LINT_CACHE="+filepath.Join(dir, ".lintcache"),
	)
	out, err := cmd.CombinedOutput()
	if err != nil {
		// Surface the command failure instead of returning text that looks like
		// "the rule did not fire" — a harness error is not a rule verdict.
		return string(out) + "\n[harness] golangci-lint error: " + err.Error() +
			"\n[harness] cmd dir=" + cmd.Dir + " args=" + strings.Join(cmd.Args, " ")
	}
	return string(out)
}

func withoutEnv(env []string, key string) []string {
	out := make([]string, 0, len(env))
	for _, kv := range env {
		if !strings.HasPrefix(kv, key+"=") {
			out = append(out, kv)
		}
	}
	return out
}
