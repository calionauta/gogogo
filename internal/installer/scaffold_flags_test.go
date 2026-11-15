// Scaffold flags: the ones that lied are gone, and the interview still asks.
//
// `--install` and `--install-recommended` were documented as installing recipes
// at scaffold time and did nothing: the plan printed a `recommend:` line and the
// files were never touched. `--recipes` replaced them with an action the tool
// can actually take (drop a demo slice), so the old spellings must stay dead.
package installer

import (
	"io"
	"os"
	"strings"
	"testing"
)

// TestRemovedScaffoldFlagsAreRejected: a caller that still passes them gets a
// parse error, not a plan that quietly ignores the intent.
func TestRemovedScaffoldFlagsAreRejected(t *testing.T) {
	for _, flag := range []string{"--install", "--install-recommended", "--skip-recommended"} {
		args := []string{"--name", "app", "--no-tui", flag}
		if flag == "--install" {
			args = append(args, "todo")
		}
		if _, err := loadOptions(args, strings.NewReader(""), io.Discard); err == nil {
			t.Errorf("%s was accepted; the flag described an action the scaffold cannot take", flag)
		}
	}
}

// TestPromptFormAsksTheShape: the shape prunes entrypoints and is genuinely the
// operator's choice, so it is asked — and Enter still gives a working default.
func TestPromptFormAsksTheShape(t *testing.T) {
	got := promptForm(strings.NewReader("myapp\nme\napi\n"), io.Discard)
	if got.name != "myapp" || got.owner != "me" || got.shape != "api" {
		t.Fatalf("promptForm = %+v", got)
	}
	if def := promptForm(strings.NewReader("\n\n\n"), io.Discard); def.shape != "full" {
		t.Errorf("default shape = %q, want full", def.shape)
	}
}

// TestFlagUsageHasNoBackticks: `flag` treats a backticked word in a usage string
// as the VALUE PLACEHOLDER, so "hand over to `make dev`" rendered the flag as
// "run make dev" and "read one with `recipe source <id>`" as "recipes recipe
// source <id>". Found by reading real --help output; a compile cannot see it.
//
// The scan is on SOURCE because the usage strings are split across concatenated
// literals: a rendered check would have to reimplement flag's parsing.
func TestFlagUsageHasNoBackticks(t *testing.T) {
	for _, file := range []string{"run.go", "usage.go", "recipe_cli.go", "recipe_status.go", "recipe_receipts.go"} {
		src, err := os.ReadFile(file)
		if err != nil {
			t.Fatalf("read %s: %v", file, err)
		}
		inDecl := false
		for i, line := range strings.Split(string(src), "\n") {
			trimmed := strings.TrimSpace(line)
			// A flag registration may span lines; track until the closing paren.
			if strings.Contains(trimmed, "fs.BoolVar(") || strings.Contains(trimmed, "fs.StringVar(") ||
				strings.Contains(trimmed, "fs.IntVar(") {
				inDecl = true
			}
			if inDecl && strings.Contains(line, "`") {
				t.Errorf("%s:%d has a backtick in a flag usage (it becomes the value placeholder): %s",
					file, i+1, trimmed)
			}
			if inDecl && strings.HasSuffix(trimmed, ")") {
				inDecl = false
			}
		}
	}
}
