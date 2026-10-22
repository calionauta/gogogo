// SCOPE:plugin — installer engine: rename must survive a fresh clone's symlinks
package installer

import (
	"os"
	"path/filepath"
	"strings"
	"testing"
)

// TestRenameTreeFollowsTheSymlinkedSkillRegistration is the regression guard
// for the fresh clone path. A real checkout registers the agent-host skills as
// SYMLINKS to directories (.agents/skills/<name> -> ../../skills/<name>), so a
// walk that treats every non-directory entry as a readable file hits EISDIR:
//
//	gogogo: read .agents/skills/gogogo-coding-standards: is a directory
//
// That made the primary scaffold path (`gogogo run` / `--yes` on a clone) fail
// on every platform, while the suite stayed green because the e2e fixture is
// synthetic and has no symlinks.
//
// Skipping the link is still right for the CONTENT pass — its target is walked
// at its own path — but it is not enough for the PATH pass: the registration
// and the skill directory both carry the project name, both move, and a link's
// target is a string no content pass can see.
func TestRenameTreeFollowsTheSymlinkedSkillRegistration(t *testing.T) {
	root := t.TempDir()

	realSkill := filepath.Join(root, "skills", "gogogo-coding-standards")
	if err := os.MkdirAll(realSkill, 0o755); err != nil {
		t.Fatal(err)
	}
	oldSkillFile := filepath.Join(realSkill, "SKILL.md")
	if err := os.WriteFile(oldSkillFile, []byte("module github.com/calionauta/gogogo\n"), 0o600); err != nil {
		t.Fatal(err)
	}

	// The shape a `git clone` produces.
	linkDir := filepath.Join(root, ".agents", "skills")
	if err := os.MkdirAll(linkDir, 0o755); err != nil {
		t.Fatal(err)
	}
	oldLink := filepath.Join(linkDir, "gogogo-coding-standards")
	relativeTarget := filepath.Join("..", "..", "skills", "gogogo-coding-standards")
	if err := os.Symlink(relativeTarget, oldLink); err != nil {
		t.Skipf("symlinks unavailable here: %v", err)
	}

	if err := renameTree(root, "my-app", "me"); err != nil {
		t.Fatalf("renameTree on a tree with a symlinked directory: %v", err)
	}

	if _, err := os.Stat(oldSkillFile); !os.IsNotExist(err) {
		t.Errorf("skills/gogogo-coding-standards still exists (err=%v) — the directory has to move", err)
	}
	got, err := os.ReadFile(filepath.Join(root, "skills", "my-app-coding-standards", "SKILL.md"))
	if err != nil {
		t.Fatalf("the moved skill file must be readable: %v", err)
	}
	if !strings.Contains(string(got), "github.com/me/my-app") {
		t.Errorf("real file was not rewritten (skipping the link must not skip the target):\n%s", got)
	}

	link := filepath.Join(linkDir, "my-app-coding-standards")
	fi, err := os.Lstat(link)
	if err != nil {
		t.Fatalf("the registration was not moved alongside its skill: %v", err)
	}
	if fi.Mode()&os.ModeSymlink == 0 {
		t.Errorf("the registration is a %v, not a symlink", fi.Mode().Type())
	}
	target, err := os.Readlink(link)
	if err != nil {
		t.Fatal(err)
	}
	if strings.Contains(target, "gogogo") {
		t.Errorf("the link target still names the old project: %q", target)
	}
	// The whole point of the registration: a host reads the skill THROUGH the
	// link. A dangling link is invisible, not an error, which is why it has to
	// be asserted.
	if _, err := os.ReadFile(filepath.Join(link, "SKILL.md")); err != nil {
		t.Errorf("the registration dangles after the rename: %v", err)
	}
}
