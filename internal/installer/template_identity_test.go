// Which tests mean something only in the TEMPLATE.
//
// A scaffolded product has already renamed every path and the module, so a test
// that asserts "cmd/gogogo moved" or "AGENTS.md records calionauta/gogogo" is
// describing a tree that no longer exists. Those tests are not weak — they are
// the template's own contract — but they have to say so, or a product ships a
// permanently red suite and everyone learns to ignore it.
package installer

import (
	"os"
	"path/filepath"
	"testing"
)

// requireTemplateIdentity skips when this checkout is not the template.
//
// The check is the module line: the template's is `github.com/calionauta/gogogo`
// and a rename is the first thing a scaffold does to it.
func requireTemplateIdentity(t *testing.T) {
	t.Helper()
	root := filepath.Join("..", "..")
	raw, err := os.ReadFile(filepath.Join(root, "go.mod"))
	if err != nil {
		t.Fatalf("read go.mod: %v", err)
	}
	if !containsModule(string(raw), templateModulePath) {
		t.Skip("this checkout is not the template: the paths asserted here were renamed by the scaffold")
	}
}

// containsModule reports whether the go.mod body declares module.
func containsModule(body, module string) bool {
	for line := range splitLines(body) {
		if len(line) > len("module ") && line[:len("module ")] == "module " {
			return line[len("module "):] == module
		}
	}
	return false
}

func splitLines(s string) func(yield func(string) bool) {
	return func(yield func(string) bool) {
		start := 0
		for i := 0; i <= len(s); i++ {
			if i == len(s) || s[i] == '\n' {
				if !yield(s[start:i]) {
					return
				}
				start = i + 1
			}
		}
	}
}
