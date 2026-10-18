// SCOPE:core — conformance guard for `gh` usage in workflows.
//
// WHY THIS EXISTS. `gh` infers the repository from the git remote, so any
// workflow step that calls it needs either a checkout or an explicit `-R`. When
// neither is present the command dies with `fatal: not a git repository`, and
// THAT failure cost eight releases: `release-please.yml`'s binary-dispatch step
// runs `gh workflow run`, it has no checkout (the release-please action needs
// none), and it only executes when `release_created == 'true'` — once per
// release, on the merge of the release PR. Every other run of that workflow was a
// green SKIP, so nothing ever reported it. v0.43.0 through v0.51.0 shipped with
// zero assets and `install.sh` 404'd for all of them.
//
// The bug was invisible for a structural reason worth mechanizing: a step that
// only fires on a rare condition has no everyday observer. This test is that
// observer.
//
// THE RULE. For each workflow file, walk its steps in order. Once a `gh <cmd>`
// invocation appears, it must be preceded (in the same job) by either an
// `actions/checkout` step or by the invocation itself carrying `-R` /
// `--repo`. Anything else is a latent "not a git repository" failure.
//
// PRECISION OVER CLEVERNESS. This parses with a line-oriented scan rather than a
// YAML decoder, because the property to check is textual and local ("does a
// checkout appear before this line in this job") and the failure mode of an
// over-eager rule is a false red on a PR that changed nothing. `gh` calls whose
// repo comes from a shell variable of the form `repos/${{ github.repository }}`
// are accepted too, since that is explicit.
package capabilities

import (
	"os"
	"path/filepath"
	"regexp"
	"strings"
	"testing"
)

// ghCallRe matches an actual invocation: the command word `gh` followed by a
// subcommand, at a shell-command position. It deliberately does NOT match prose
// ("`gh` infers the repository…") or a comment, so a doc line cannot satisfy or
// trip the guard.
var ghCallRe = regexp.MustCompile(`(^|[;&|]\s*|\$\(|\beval\s+|\brun:\s*)gh\s+[a-z]`)

// jobRe matches a job key at the mapping indent used by these workflows.
var jobRe = regexp.MustCompile(`^  [a-z0-9_-]+:\s*$`)

// TestGhInvocationsHaveARepository is the guard. See the file header for why.
func TestGhInvocationsHaveARepository(t *testing.T) {
	t.Parallel()
	wfDir := filepath.Join(repoRoot(t), ".github", "workflows")
	entries, err := os.ReadDir(wfDir)
	if err != nil {
		t.Fatalf("read %s: %v", wfDir, err)
	}
	var checked int
	for _, e := range entries {
		if e.IsDir() || !strings.HasSuffix(e.Name(), ".yml") {
			continue
		}
		rel := filepath.Join(".github", "workflows", e.Name())
		raw, rerr := os.ReadFile(filepath.Join(wfDir, e.Name()))
		if rerr != nil {
			t.Fatalf("read %s: %v", rel, rerr)
		}
		checked++
		checkWorkflowGhCalls(t, rel, string(raw))
	}
	if checked == 0 {
		t.Fatal("no workflow files found — the guard is checking nothing")
	}
}

// checkWorkflowGhCalls walks one workflow's lines and reports every `gh`
// invocation that lacks a preceding checkout in its job.
func checkWorkflowGhCalls(t *testing.T, rel, body string) {
	t.Helper()
	lines := strings.Split(body, "\n")

	haveCheckout := false
	inJob := ""
	// A `run:` block scalar's CONTENT is where a multi-line `gh` call lives
	// (both the release-please bridge and the asset verification are block
	// scalars), so the scan is line-oriented over every line rather than over
	// parsed steps: the property being checked is textual and local.
	for i, line := range lines {
		if jobRe.MatchString(line) {
			haveCheckout = false
			inJob = strings.TrimSpace(line)
		}
		if strings.Contains(line, "actions/checkout") {
			haveCheckout = true
		}
		if !ghCallRe.MatchString(line) {
			continue
		}
		// Explicit repo on the invocation itself.
		if strings.Contains(line, "-R ") || strings.Contains(line, "--repo") ||
			strings.Contains(line, "repos/${{") || strings.Contains(line, "repos/${") {
			continue
		}
		if haveCheckout {
			continue
		}
		t.Errorf("%s:%d: `gh` invocation with no repository to infer and no `-R`.\n"+
			"  job: %s\n  line: %s\n"+
			"  `gh` reads the repo from the git remote, so a step needs an "+
			"`actions/checkout` before it or `-R \"$GITHUB_REPOSITORY\"` on the "+
			"invocation. Without one it fails with `fatal: not a git repository` — "+
			"which is how eight releases shipped with zero assets.",
			rel, i+1, inJob, strings.TrimSpace(line))
	}
}
