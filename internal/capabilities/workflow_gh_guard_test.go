// SCOPE:core — conformance guard for `gh` usage in workflows.
//
// WHY THIS EXISTS. `gh` infers the repository from the git remote, so a workflow
// step that calls it needs either a checkout or an explicit `-R`. Without one the
// command dies with `fatal: not a git repository`, and THAT failure cost eight
// releases: `release-please.yml`'s binary-dispatch step runs `gh workflow run`,
// it has no checkout (the release-please action needs none), and it only executes
// when `release_created == 'true'` — once per release, on the merge of the
// release PR. Every other run of that workflow was a green SKIP, so nothing ever
// reported it. v0.43.0 through v0.51.0 shipped with zero assets and `install.sh`
// 404'd for all of them.
//
// The bug survived for a structural reason worth mechanizing: a step that only
// fires on a rare condition has no everyday observer. This test is that observer.
//
// WHY THIS PARSES YAML INSTEAD OF SCANNING LINES. The first version of this guard
// walked the file line by line with regexes keyed on indentation and substring
// matching. That is wrong for a structural reason — YAML's surface syntax changes
// shape without changing meaning:
//
//	run: |            block scalar, the command is on the FOLLOWING lines
//	run: >            folded scalar, same
//	run: "gh pr view" the whole command is one quoted line, indentation-agnostic
//	- {uses: x, run: gh pr view}   flow mapping, one line, no `- ` block
//	steps: *shared   anchors/aliases, so the steps are not even at this node
//	run: |           and inside it, a `gh` call under an `if` or in a heredoc
//
// A line-oriented reader sees the wrong thing for every one of those and so
// either misses a real case or invents one. This test's subject is a DATA
// STRUCTURE — which steps in which job, and what each step's command is — so it
// decodes the document and walks that. Same reasoning as
// cmd/check-skill-frontmatter, which uses gopkg.in/yaml.v3 for exactly this
// ("a regex would only catch the one shape already seen"); yaml.v3 is already in
// the build graph, so this adds no dependency.
//
// THE RULE is about the STRUCTURE, not the text layout: within one job, walking
// the steps in order, a step whose command invokes `gh` must either be preceded by
// a step using `actions/checkout`, or name its repository itself (`-R` / `--repo`,
// or a `repos/<owner>/<name>` path).
//
// The rule engine is split from the file walker (checkWorkflow / ghViolations) so
// the adversarial shapes above are pinned by fixtures instead of being argued
// about. See workflow_gh_guard_fixtures_test.go.
package capabilities

import (
	"fmt"
	"os"
	"path/filepath"
	"regexp"
	"sort"
	"strings"
	"testing"

	"gopkg.in/yaml.v3"
)

// ghCommandRe matches an actual invocation: the command word `gh` followed by a
// subcommand name, at a shell-command position — start of the script, or after a
// separator, a command substitution, or a prefix that keeps `gh` as the command
// (`eval`, `env` with assignments). Anchoring on a command position is what stops
// prose like "`gh` infers the repository" from matching. Comments are invisible
// here for free, because the YAML parser already removed them.
//
// The captured group is the SUBCOMMAND, because whether a repository can be
// inferred depends on it — see repoScopedSubcommands.
const ghPos = `(^|[\n;&|]\s*|\$\(|\beval\s+|` +
	`\benv\s+(?:[A-Za-z_][A-Za-z0-9_]*=\S*\s+)*)`

var ghCommandRe = regexp.MustCompile(ghPos + `gh\s+([a-z][a-z0-9-]*)`)

// repoScopedSubcommands are the `gh` subcommands that resolve a repository from
// the working directory, and therefore FAIL on a runner with no checkout. Only
// these can produce `fatal: not a git repository`, so only these are governed by
// the rule.
//
// The distinction is the whole reason this list exists rather than "any `gh`":
// `gh auth status`, `gh extension list`, `gh config get` and `gh version` are
// commonly used in workflows precisely BECAUSE they are repository-independent,
// and flagging them is how a guard earns a reputation for being wrong. Measured
// before this split: those three were reported as violations, i.e. the guard was
// telling authors to add a checkout they do not need.
//
// `api` is deliberately included but handled with a second check, because
// `gh api /user` needs no repository while `gh api repos/x/y/...` names one in the
// path; both are correct, and only an `api` call that is neither repo-scoped in
// the path nor explicitly given `-R` is a finding.
var repoScopedSubcommands = map[string]bool{
	"api": true, "cache": true, "codespace": true, "gist": false,
	"issue": true, "label": true, "org": false, "pr": true,
	"project": true, "release": true, "repo": true,
	"run": true, "secret": true, "variable": true, "workflow": true,
}

// repoIndependentSubcommands is NOT declared as a map: the comment on
// repoScopedSubcommands already records the other half, and a second structure
// that nothing reads would be documentation pretending to be code (it was, and
// `unused` flagged it). The measured cases are pinned as fixtures instead — see
// TestGhGuardRepositoryIndependentSubcommandsDoNotRequireACheckout.

// explicitRepoRe matches a repository named on the command itself rather than
// inferred from a checkout: `-R owner/name`, `--repo=owner/name`, or a REST path
// containing `repos/`.
var explicitRepoRe = regexp.MustCompile(`(?:^|[\s=])(?:-R|--repo)[\s=]|repos/`)

// violationTmpl is the message for one unresolvable `gh` call. Kept as a const so
// the long line does not need splitting mid-sentence.
// violationTmpl is the message for one unresolvable `gh` call. Arguments, in
// order: file, job, step number, step label, subcommand, command. Kept as a
// const so the long text does not need splitting mid-sentence.
const violationTmpl = `%s: job %q step %d (%s): gh %s resolves a repository, and none can be inferred.
` +
	`  command: %s
` +
	`  gh reads the repository from the git remote, so the step needs an` +
	` actions/checkout before it, or -R "$GITHUB_REPOSITORY" on the invocation` +
	` itself. Without one it fails with "fatal: not a git repository" — which is` +
	` how v0.43.0 through v0.51.0 shipped with zero assets.`

// workflowDoc is the part of a workflow this guard reads. Fields are decoded
// loosely so any valid workflow parses, including keys this guard does not model.
type workflowDoc struct {
	Jobs map[string]workflowJob `yaml:"jobs"`
}

type workflowJob struct {
	Steps []workflowStep `yaml:"steps"`
}

// workflowStep decodes the three step shapes a workflow may use. `Uses` is `any`
// because YAML resolves an unquoted `on:`-style scalar oddly and a caller might
// write a mapping there; decodeUses normalizes it.
type workflowStep struct {
	Name string `yaml:"name"`
	Uses any    `yaml:"uses"`
	Run  string `yaml:"run"`
}

// TestGhInvocationsHaveARepository is the guard over the real tree.
func TestGhInvocationsHaveARepository(t *testing.T) {
	t.Parallel()
	wfDir := filepath.Join(repoRoot(t), ".github", "workflows")
	entries, err := os.ReadDir(wfDir)
	if err != nil {
		t.Fatalf("read %s: %v", wfDir, err)
	}
	var files, invoking int
	for _, e := range entries {
		if e.IsDir() || !isYAML(e.Name()) {
			continue
		}
		rel := filepath.Join(".github", "workflows", e.Name())
		raw, rerr := os.ReadFile(filepath.Join(wfDir, e.Name()))
		if rerr != nil {
			t.Fatalf("read %s: %v", rel, rerr)
		}
		if errors := checkWorkflow(rel, raw); len(errors) > 0 {
			for _, msg := range errors {
				t.Error(msg)
			}
		}
		files++
		if invokesGH(raw) {
			invoking++
		}
	}
	if files == 0 {
		t.Fatal("no workflow files found — the guard is checking nothing")
	}
	// A matcher that silently stopped matching is indistinguishable from a clean
	// tree, which is the exact failure class this file exists to prevent. So if
	// the tree currently calls `gh` anywhere, the guard must be seeing it.
	if invoking == 0 {
		t.Fatalf("scanned %d workflow file(s) and found no `gh` invocation at all — "+
			"either the tree stopped using `gh` (then delete this guard) or the "+
			"matcher broke (then fix it)", files)
	}
	t.Logf("scanned %d workflow file(s); %d invokes `gh`", files, invoking)
}

// checkWorkflow parses one workflow and returns a message per violation.
//
// Split from the walker so fixtures can drive it — including documents that are
// not valid YAML, which is itself a finding (GitHub would reject the file).
func checkWorkflow(rel string, raw []byte) []string {
	var doc workflowDoc
	if err := yaml.Unmarshal(raw, &doc); err != nil {
		return []string{rel + ": not valid YAML: " + err.Error()}
	}
	return ghViolations(rel, doc)
}

// ghViolations walks the decoded jobs and returns a message per `gh` CALL that
// cannot resolve a repository. Jobs are visited in sorted order, and each `gh`
// invocation inside a step is judged on its own subcommand, so
// `gh auth status && gh pr view 1` flags only the second. Output is
// deterministic.
func ghViolations(rel string, doc workflowDoc) []string {
	jobNames := make([]string, 0, len(doc.Jobs))
	for name := range doc.Jobs {
		jobNames = append(jobNames, name)
	}
	sort.Strings(jobNames)

	var out []string
	for _, jobName := range jobNames {
		job := doc.Jobs[jobName]
		hasCheckout := false
		for i, step := range job.Steps {
			if strings.HasPrefix(decodeUses(step.Uses), "actions/checkout") {
				hasCheckout = true
			}
			for _, cmd := range ghCommands(step.Run) {
				if !cmd.needsRepo() {
					continue
				}
				if hasCheckout || explicitRepoRe.MatchString(cmd.text) {
					continue
				}
				out = append(out, fmt.Sprintf(violationTmpl,
					rel, jobName, i+1, stepLabel(step), cmd.sub, firstLine(step.Run)))
			}
		}
	}
	return out
}

// ghCmd is one `gh` invocation: its subcommand, and the command text it was
// found in (used for the explicit-repository check).
type ghCmd struct {
	sub  string
	text string
}

// needsRepo reports whether this call resolves a repository from the working
// directory, and therefore fails on a runner with no checkout.
func (c ghCmd) needsRepo() bool {
	if !repoScopedSubcommands[c.sub] {
		return false
	}
	// `gh api` is the one form where the repository can live in the PATH rather
	// than the working directory: `gh api /user` needs none, `gh api
	// repos/x/y/...` names its own. Only the repository-independent form is
	// exempt here; an explicit `repos/` path passes the check below anyway.
	if c.sub == "api" && !strings.Contains(c.text, "repos/") {
		return false
	}
	return true
}

// ghCommands extracts every `gh <subcommand>` from a step's script so each is
// judged separately. A global flag is skipped rather than mistaken for a
// subcommand (`gh --version`).
//
// It uses FindAllStringSubmatchIndex (not ...Submatch) because the explicit-
// repository check needs the byte OFFSET of each match: the scan for `-R` must
// start at the invocation, not at the start of the script.
func ghCommands(script string) []ghCmd {
	if script == "" {
		return nil
	}
	var out []ghCmd
	// Each match has 3 groups: whole match, prefix, subcommand -> 6 indices.
	for _, idx := range ghCommandRe.FindAllStringSubmatchIndex(script, -1) {
		if len(idx) < 6 || idx[4] < 0 || idx[5] < 0 {
			continue
		}
		sub := script[idx[4]:idx[5]]
		if strings.HasPrefix(sub, "-") {
			continue
		}
		// Scan from this match to the end of its LOGICAL LINE, so a `-R` later in
		// the same command counts while one in an unrelated later command does
		// not. A line continuation (trailing `\`) keeps the scan going.
		rest := logicalLine(script[idx[0]:])
		out = append(out, ghCmd{sub: sub, text: rest})
	}
	return out
}

// logicalLine returns the first logical line of s, joining continuations.
func logicalLine(s string) string {
	var b strings.Builder
	for line := range strings.SplitSeq(s, "\n") {
		b.WriteString(line)
		b.WriteByte(' ')
		if !strings.HasSuffix(strings.TrimRight(line, " \t"), "\\") {
			break
		}
	}
	return b.String()
}

// invokesGH reports whether any step runs a repository-scoped `gh` subcommand —
// i.e. whether the guard had anything to judge. Used by the self-check that the
// matcher still matches something.
func invokesGH(raw []byte) bool {
	var doc workflowDoc
	if err := yaml.Unmarshal(raw, &doc); err != nil {
		return false
	}
	for _, job := range doc.Jobs {
		for _, step := range job.Steps {
			for _, cmd := range ghCommands(step.Run) {
				if cmd.needsRepo() {
					return true
				}
			}
		}
	}
	return false
}

// decodeUses normalizes a step's `uses` value to a string. A mapping there is
// malformed for a step (GitHub expects a scalar) but must not panic the guard.
func decodeUses(v any) string {
	s, _ := v.(string)
	return s
}

// stepLabel names a step for a failure message, falling back to its command when
// the workflow left it unnamed (which is legal).
func stepLabel(step workflowStep) string {
	if step.Name != "" {
		return step.Name
	}
	if u := decodeUses(step.Uses); u != "" {
		return "uses " + u
	}
	return "unnamed step"
}

// firstLine keeps a multi-line `run:` from flooding the failure output.
func firstLine(s string) string {
	s = strings.TrimSpace(s)
	if i := strings.IndexByte(s, '\n'); i >= 0 {
		return "[" + strings.TrimSpace(s[:i]) + " …]"
	}
	return s
}

// isYAML reports whether a filename is a YAML workflow.
func isYAML(name string) bool {
	return strings.HasSuffix(name, ".yml") || strings.HasSuffix(name, ".yaml")
}
