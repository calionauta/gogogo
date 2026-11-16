package main

import (
	"strings"
	"testing"
)

// TestCheckCLIFlagsNamesAnUndocumentedFlag is the rule's red side: a flag the
// installer registers and the page never names is a finding.
func TestCheckCLIFlagsNamesAnUndocumentedFlag(t *testing.T) {
	root := t.TempDir()
	writeFixture(t, root, "internal/installer/run.go", `package installer

func bind(fs *flag.FlagSet) {
	fs.StringVar(&opt.recipes, "recipes", "none", "which demos to drop")
	fs.BoolVar(&opt.yes, "yes", false, "never prompt")
}
`)
	writeFixture(t, root, "docs/cli.md", "Run `gogogo --yes` to scaffold.\n")

	problems := checkCLIFlags(root)
	if len(problems) != 1 {
		t.Fatalf("problems = %v, want exactly the undocumented --recipes", problems)
	}
	if !strings.Contains(problems[0], "--recipes") {
		t.Fatalf("problem %q does not name the flag", problems[0])
	}
}

// TestCheckCLIFlagsAcceptsANamedFlag is the green side: naming the flag anywhere
// in the page satisfies the rule, whatever the sentence.
func TestCheckCLIFlagsAcceptsANamedFlag(t *testing.T) {
	root := t.TempDir()
	writeFixture(t, root, "internal/installer/run.go", `package installer

func bind(fs *flag.FlagSet) {
	fs.StringVar(&opt.recipes, "recipes", "none", "which demos to drop")
}
`)
	writeFixture(t, root, "docs/cli.md", "Pass `--recipes all` to keep every slice.\n")

	if problems := checkCLIFlags(root); len(problems) != 0 {
		t.Fatalf("problems = %v, want none", problems)
	}
}

// TestCheckCLIFlagsFailsWhenTheParserFindsNothing pins the "a gate that silently
// checks nothing" failure: an installer with no parseable registration must be
// an error, not an empty finding list.
func TestCheckCLIFlagsFailsWhenTheParserFindsNothing(t *testing.T) {
	root := t.TempDir()
	writeFixture(t, root, "internal/installer/run.go", "package installer\n")
	writeFixture(t, root, "docs/cli.md", "nothing here\n")

	problems := checkCLIFlags(root)
	if len(problems) != 1 || !strings.Contains(problems[0], "parser broke") {
		t.Fatalf("problems = %v, want the parser-broke error", problems)
	}
}
