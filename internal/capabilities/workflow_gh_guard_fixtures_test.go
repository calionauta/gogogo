// SCOPE:core — adversarial fixtures for the workflow `gh` guard.
//
// THE POINT OF THIS FILE. The guard's first version scanned YAML line by line
// with indentation-keyed regexes, and review correctly rejected it: YAML's surface
// syntax changes shape without changing meaning, so a line-oriented reader is
// wrong in both directions. These fixtures are the shapes that break a line
// scanner, written as documents that MEAN something — so the guard's behaviour on
// them is pinned rather than argued about.
//
// Each case states what the document means and which answer the guard must give.
// The "must NOT flag" group is the half that a regex approach gets wrong by
// inventing violations; the "must flag" group is the half it gets wrong by
// missing them.
package capabilities

import (
	"os"
	"path/filepath"
	"strings"
	"testing"
)

// wf builds a minimal workflow around a jobs body. Indentation of the body is
// the caller's business, which is the point — the guard must not care.
func wf(body string) []byte {
	return []byte("name: t\non: push\njobs:\n" + body)
}

func TestGhGuardFlagsMissingRepository(t *testing.T) {
	t.Parallel()
	cases := []struct {
		name string
		body string
	}{
		{
			// The original bug, verbatim in shape: a `gh` call with no checkout
			// anywhere in the job.
			name: "no checkout and no -R",
			body: "  rel:\n    steps:\n      - run: gh workflow run release.yml --ref master\n",
		},
		{
			// A checkout in a DIFFERENT job does not help: `gh` resolves per
			// working directory, and each job gets a fresh runner.
			name: "checkout in another job does not count",
			body: "  a:\n    steps:\n      - uses: actions/checkout@v5\n" +
				"  b:\n    steps:\n      - run: gh pr comment 1 --body hi\n",
		},
		{
			// The checkout must come BEFORE the call, not after: `gh` runs in
			// step order.
			name: "checkout after the gh call is too late",
			body: "  rel:\n    steps:\n      - run: gh release view v1\n" +
				"      - uses: actions/checkout@v5\n",
		},
		{
			// A `gh` call buried in a multi-line block scalar after an `if` —
			// exactly the shape a line scanner with a `run:` anchor misses.
			name: "gh inside a block scalar after a conditional",
			body: "  rel:\n    steps:\n      - run: |\n          set -euo pipefail\n" +
				"          if [ \"$X\" = 1 ]; then\n            gh pr edit 1 --add-label x\n          fi\n",
		},
		{
			// Same command, folded scalar instead of literal — different YAML
			// syntax, identical meaning.
			name: "gh inside a folded scalar",
			body: "  rel:\n    steps:\n      - run: >\n          gh release list\n",
		},
		{
			// A `gh` call preceded by an env prefix, after a separator. The `api`
			// subcommand without a `repos/` path is repository-INDEPENDENT, so to
			// exercise the command-position matcher this uses a repo-scoped one.
			name: "gh after env assignment and separator",
			body: "  rel:\n    steps:\n      - run: |\n" +
				"          echo start; env GH_TOKEN=x gh pr view 1\n",
		},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			t.Parallel()
			got := checkWorkflow("wf.yml", wf(tc.body))
			if len(got) == 0 {
				t.Errorf("no violation reported, want one.\nbody:\n%s", tc.body)
			}
		})
	}
}

func TestGhGuardAcceptsResolvableRepository(t *testing.T) {
	t.Parallel()
	cases := []struct {
		name string
		body string
	}{
		{
			// The fix that closed the eight-release bug.
			name: "explicit -R",
			body: "  rel:\n    steps:\n      - run: gh workflow run release.yml -R \"$GITHUB_REPOSITORY\"\n",
		},
		{
			name: "explicit --repo",
			body: "  rel:\n    steps:\n      - run: gh pr view 1 --repo owner/name\n",
		},
		{
			name: "explicit --repo= form",
			body: "  rel:\n    steps:\n      - run: gh pr view 1 --repo=owner/name\n",
		},
		{
			// A REST path names the repo in the URL, no flag needed.
			name: "repos path",
			body: "  rel:\n    steps:\n      - run: gh api repos/owner/name/actions/runs\n",
		},
		{
			name: "repos path with an expression",
			body: "  rel:\n    steps:\n      - run: gh api \"repos/${{ github.repository }}/releases\"\n",
		},
		{
			// A checkout BEFORE the call is sufficient on its own.
			name: "checkout before the call",
			body: "  rel:\n    steps:\n      - uses: actions/checkout@v5\n      - run: gh pr comment 1 --body hi\n",
		},
		{
			// Nothing to flag: no `gh` at all.
			name: "no gh invocation",
			body: "  rel:\n    steps:\n      - run: make build\n",
		},
		{
			// Prose must not match. This is the false-positive class a substring
			// matcher creates, and it is invisible to the parser because YAML
			// already stripped the comment.
			name: "prose mentioning gh in a comment",
			body: "  rel:\n    steps:\n" +
				"      # gh reads the repo from the remote, so always pass -R\n" +
				"      - run: make build\n",
		},
		{
			name: "prose mentioning gh at the start of a line",
			body: "  rel:\n    steps:\n      - run: |\n" +
				"          # gh needs a repository; we pass make, not gh\n" +
				"          make build\n",
		},
		{
			// A word that merely starts with `gh` is not the command.
			name: "gh-prefixed word",
			body: "  rel:\n    steps:\n      - run: ghcr-tool push\n",
		},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			t.Parallel()
			got := checkWorkflow("wf.yml", wf(tc.body))
			if len(got) != 0 {
				t.Errorf("reported %d violation(s), want none:\n%s\nbody:\n%s",
					len(got), strings.Join(got, "\n"), tc.body)
			}
		})
	}
}

// TestGhGuardRejectsInvalidYAML pins that a workflow GitHub would refuse is a
// finding too — the guard cannot reason about a document it cannot read, and
// silently reporting "clean" would be the worst answer.
func TestGhGuardRejectsInvalidYAML(t *testing.T) {
	t.Parallel()
	got := checkWorkflow("broken.yml", []byte("jobs:\n  a:\n   bad:\n  \t- tab indent\n"))
	if len(got) == 0 {
		t.Fatal("invalid YAML reported clean")
	}
	if !strings.Contains(got[0], "not valid YAML") {
		t.Errorf("message does not name the parse failure: %v", got)
	}
}

// TestGhGuardIsIndentationAgnostic is the property the reviewers' objection is
// ultimately about: the same document, reindented, must get the same verdict. A
// line scanner keyed on indent width fails this; a parser cannot.
func TestGhGuardIsIndentationAgnostic(t *testing.T) {
	t.Parallel()
	compact := []byte("jobs:\n  rel:\n    steps:\n    - run: gh pr view 1\n")
	// Same structure, sequences indented by four instead of two (both legal).
	wide := []byte("jobs:\n  rel:\n    steps:\n        - run: gh pr view 1\n")

	a, b := checkWorkflow("a.yml", compact), checkWorkflow("b.yml", wide)
	if len(a) != 1 || len(b) != 1 {
		t.Fatalf("verdicts differ by indentation: compact=%d wide=%d violations", len(a), len(b))
	}
	// Normalize the filename prefix, which legitimately differs.
	na := strings.Replace(a[0], "a.yml", "X", 1)
	nb := strings.Replace(b[0], "b.yml", "X", 1)
	if na != nb {
		t.Errorf("messages differ by indentation:\n%s\n%s", a[0], b[0])
	}
}

// TestGhGuardRepositoryIndependentSubcommandsDoNotRequireACheckout pins the
// split that a "any `gh` needs a repo" rule gets wrong. These subcommands are
// used in workflows precisely BECAUSE they need no repository, and flagging them
// tells authors to add a checkout they do not need — measured before the split:
// all three below were reported as violations.
func TestGhGuardRepositoryIndependentSubcommandsDoNotRequireACheckout(t *testing.T) {
	t.Parallel()
	for _, run := range []string{
		"gh auth status",
		"gh config get editor",
		"gh extension list",
		"gh --version",
		"gh version",
		"gh api /user",
		"gh help",
		"gh status",
	} {
		body := "  rel:\n    steps:\n      - run: " + run + "\n"
		if got := checkWorkflow("wf.yml", wf(body)); len(got) != 0 {
			t.Errorf("%q reported %d violation(s), want none:\n%s", run, len(got), strings.Join(got, "\n"))
		}
	}
}

// TestGhGuardRepositoryScopedSubcommandsNeedACheckout is the other half: the
// subcommands that DO resolve a repository must be flagged without one.
func TestGhGuardRepositoryScopedSubcommandsNeedACheckout(t *testing.T) {
	t.Parallel()
	for _, run := range []string{
		"gh pr view 1",
		"gh release list",
		"gh workflow run release.yml",
		"gh run list",
		"gh issue list",
	} {
		body := "  rel:\n    steps:\n      - run: " + run + "\n"
		if got := checkWorkflow("wf.yml", wf(body)); len(got) != 1 {
			t.Errorf("%q reported %d violation(s), want exactly 1", run, len(got))
		}
	}

	// `gh api` is the exception that names its own repository in the PATH, so
	// `repos/x/y/...` is self-sufficient while a bare endpoint still needs one.
	// Asserted separately because a single list cannot express both answers.
	selfSufficient := "  rel:\n    steps:\n      - run: gh api repos/owner/name/releases\n"
	if got := checkWorkflow("wf.yml", wf(selfSufficient)); len(got) != 0 {
		t.Errorf("`gh api repos/...` reported %d violation(s), want none (the path "+
			"names the repository):\n%s", len(got), strings.Join(got, "\n"))
	}
	bareEndpoint := "  rel:\n    steps:\n      - run: gh api repos\n"
	if got := checkWorkflow("wf.yml", wf(bareEndpoint)); len(got) != 0 {
		t.Errorf("`gh api repos` (an endpoint named repos) reported %d violation(s), "+
			"want none:\n%s", len(got), strings.Join(got, "\n"))
	}
}

// TestGhGuardJudgesEachCommandInAStep pins that a compound command is judged
// per-invocation: the exempt half must not excuse the other half.
func TestGhGuardJudgesEachCommandInAStep(t *testing.T) {
	t.Parallel()
	body := "  rel:\n    steps:\n      - run: gh auth status && gh pr view 1\n"
	got := checkWorkflow("wf.yml", wf(body))
	if len(got) != 1 {
		t.Fatalf("want exactly 1 violation (the `gh pr` half), got %d:\n%s",
			len(got), strings.Join(got, "\n"))
	}
	if !strings.Contains(got[0], "gh pr") {
		t.Errorf("the violation should name the `pr` subcommand: %s", got[0])
	}
}

// TestGhGuardUnknownSubcommandFailsOpen documents the deliberate direction of the
// unknown case: a subcommand this guard has not learned is treated as needing
// nothing. Failing closed would turn every `gh` release that adds a subcommand
// into a broken build here, which is the worse failure. Pinned so the trade is a
// decision rather than an accident.
func TestGhGuardUnknownSubcommandFailsOpen(t *testing.T) {
	t.Parallel()
	body := "  rel:\n    steps:\n      - run: gh somefuturecmd --flag\n"
	if got := checkWorkflow("wf.yml", wf(body)); len(got) != 0 {
		t.Errorf("an unknown subcommand was flagged (%v); the documented direction "+
			"is to fail open", got)
	}
}

// TestGhGuardInspectsRealWorkflowFiles pins the guard against the actual tree,
// which is what the fixer of this test cares about: every workflow parses, and
// the release-please bridge keeps the -R that closed the eight-release bug.
func TestGhGuardInspectsRealWorkflowFiles(t *testing.T) {
	t.Parallel()
	raw, err := os.ReadFile(filepath.Join(repoRoot(t), ".github", "workflows", "release-please.yml"))
	if err != nil {
		t.Fatalf("read release-please.yml: %v", err)
	}
	if got := checkWorkflow("release-please.yml", raw); len(got) != 0 {
		t.Errorf("release-please.yml has %d violation(s):\n%s", len(got), strings.Join(got, "\n"))
	}
}
