package main

import (
	"os"
	"path/filepath"
	"strings"
	"testing"
)

// TestCLIVerbsAreNamed pins the rule in both directions: a verb the
// reference page names is fine, a verb it does not name is reported.
func TestCLIVerbsAreNamed(t *testing.T) {
	root := t.TempDir()
	writeFixture(t, root, cliReferencePage, ""+
		"| `gogogo new feature <name>` | Yes | Scaffold |\n"+
		"| `gogogo recipe install <dir>` | Yes | Install |\n"+
		"| `gogogo recipe describe <id>` | No | Describe |\n")

	problems := checkCLIVerbs(root, []string{"new", "doctor"}, []string{"install", "describe", "check"})
	if len(problems) != 2 {
		t.Fatalf("expected the doctor and the recipe check verbs to be missing, got %v", problems)
	}
	if !strings.Contains(problems[0], "doctor") {
		t.Errorf("the missing verb is not named: %v", problems[0])
	}
	if !strings.Contains(problems[1], "recipe check") {
		t.Errorf("the missing recipe verb is not named: %v", problems[1])
	}
}

// TestCLIVerbsReadsBothSpellings pins the two shapes a documented command
// takes: `gogogo <verb>` for the top level and `gogogo recipe <verb>` for the
// recipe group. A page that writes the noun must not silence the verb.
func TestCLIVerbsReadsBothSpellings(t *testing.T) {
	root := t.TempDir()
	writeFixture(t, root, cliReferencePage, ""+
		"| `go run ./cmd/gogogo recipe list --dir .` | No | List |\n"+
		"| `gogogo doctor --json` | No | Preflight |\n")

	problems := checkCLIVerbs(root, []string{"doctor"}, []string{"list", "invoke"})
	if len(problems) != 1 || !strings.Contains(problems[0], "invoke") {
		t.Fatalf("expected only the invoke verb to be missing, got %v", problems)
	}
}

// TestRecipeVerbsAreReadFromTheDispatch pins the reader: the verbs come from
// the dispatch switch, and the alias case contributes only its canonical
// name.
func TestRecipeVerbsAreReadFromTheDispatch(t *testing.T) {
	root := t.TempDir()
	writeFixture(t, root, "internal/installer/recipe_cli.go", ""+
		"package installer\n\nfunc runRecipe() {\n\tswitch args[0] {\n"+
		"\tcase \"install\":\n\t\treturn nil\n"+
		"\tcase \"describe\", \"show\":\n\t\treturn nil\n"+
		"\t}\n}\n")

	verbs, err := recipeVerbs(root)
	if err != nil {
		t.Fatalf("read the recipe verbs: %v", err)
	}
	if len(verbs) != 2 || verbs[0] != "describe" || verbs[1] != "install" {
		t.Fatalf("verbs = %v, want [describe install]", verbs)
	}
}

// writeFixture writes one file under a root, creating the directories.
func writeFixture(t *testing.T, root, rel, body string) {
	t.Helper()
	full := filepath.Join(root, filepath.FromSlash(rel))
	if err := os.MkdirAll(filepath.Dir(full), 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(full, []byte(body), 0o600); err != nil {
		t.Fatal(err)
	}
}
