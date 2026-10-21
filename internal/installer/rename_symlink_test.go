// SCOPE:plugin — installer engine: rename must survive a fresh clone's symlinks
package installer

import (
	"os"
	"path/filepath"
	"strings"
	"testing"
)

// TestRenameTreeSkipsSymlinkedDirectories is the regression guard for the fresh
// clone path. A real checkout registers the agent-host skills as SYMLINKS to
// directories (.agents/skills/<name> -> ../../skills/<name>), so a walk that
// treats every non-directory entry as a readable file hits EISDIR:
//
//	gogogo: read .agents/skills/gogogo-coding-standards: is a directory
//
// That made the primary scaffold path (`gogogo run` / `--yes` on a clone) fail
// on every platform, while the suite stayed green because the e2e fixture is
// synthetic and has no symlinks.
func TestRenameTreeSkipsSymlinkedDirectories(t *testing.T) {
	root := t.TempDir()

	// The real skill content carries the module path, so skipping the link must
	// not skip the rewrite: the target is walked at its own path.
	realSkill := filepath.Join(root, "skills", "gogogo-coding-standards")
	if err := os.MkdirAll(realSkill, 0o755); err != nil {
		t.Fatal(err)
	}
	skillFile := filepath.Join(realSkill, "SKILL.md")
	if err := os.WriteFile(skillFile, []byte("module github.com/calionauta/gogogo\n"), 0o600); err != nil {
		t.Fatal(err)
	}

	// The shape a `git clone` produces.
	linkDir := filepath.Join(root, ".agents", "skills")
	if err := os.MkdirAll(linkDir, 0o755); err != nil {
		t.Fatal(err)
	}
	link := filepath.Join(linkDir, "gogogo-coding-standards")
	if err := os.Symlink(filepath.Join("..", "..", "skills", "gogogo-coding-standards"), link); err != nil {
		t.Skipf("symlinks unavailable here: %v", err)
	}

	if err := renameTree(root, "my-app", "me"); err != nil {
		t.Fatalf("renameTree on a tree with a symlinked directory: %v", err)
	}

	got, err := os.ReadFile(skillFile)
	if err != nil {
		t.Fatalf("the real skill file must still be readable: %v", err)
	}
	if !strings.Contains(string(got), "github.com/me/my-app") {
		t.Errorf("real file was not rewritten (skipping the link must not skip the target):\n%s", got)
	}

	fi, err := os.Lstat(link)
	if err != nil {
		t.Fatalf("the registration symlink must be left alone: %v", err)
	}
	if fi.Mode()&os.ModeSymlink == 0 {
		t.Errorf("the symlink was replaced by a %v entry", fi.Mode().Type())
	}
}
