// SCOPE:plugin — installer engine: project rename (module path + references)
package installer

import (
	"fmt"
	"os"
	"path/filepath"
	"slices"
	"strings"
)

// binarySniffLen bounds the binary sniff: NUL bytes past this window do not
// matter because the rewrite only touches text-like tracked sources.
const binarySniffLen = 8192

// renameOldName is the template's name, the string a rename removes from both
// the CONTENTS and the PATHS.
const renameOldName = "gogogo"

// renameTree ports the scripts/rename-project.py rules: module path first,
// bare name second, Pages host only on owner change, and then the paths.
// Shields sibling repos, skips generated site/docs output. Operates on tracked
// files when inside a git checkout, otherwise walks the tree skipping .git.
func renameTree(root, newName, newOwner string) error {
	const oldName = renameOldName
	const oldOwner = "calionauta"
	oldModule := "github.com/" + oldOwner + "/" + oldName
	newModule := "github.com/" + newOwner + "/" + newName

	foreign := []string{
		"github.com/calionauta/ai-credits",
		"github.com/calionauta/datastar-lint",
		"github.com/calionauta/pi-leakguard",
	}

	t, err := openTree(root)
	if err != nil {
		return err
	}
	defer t.Close()
	files, err := collectRenameFiles(root)
	if err != nil {
		return err
	}

	for _, p := range files {
		if err := rewriteRenameFile(t, p, oldModule, newModule, oldOwner, oldName, newOwner, newName, foreign); err != nil {
			return err
		}
	}

	// The content pass rewrote every REFERENCE to a name-bearing path
	// (`./cmd/gogogo` became `./cmd/my-app`, the pin surface became
	// `cmd/my-app-mcp/go.mod`). Leaving the directories named gogogo makes each
	// of those references point at nothing -- and the build proof stays green,
	// because it never opens them.
	if err := moveRenamePaths(t, root, newName); err != nil {
		return err
	}
	return repointRenameSymlinks(t, root, newName)
}

// moveRenamePaths moves every path whose NAME carries the template name:
// cmd/gogogo, cmd/gogogo-mcp and skills/gogogo-coding-standards. Only the
// shallowest such prefix moves, so a directory moves once instead of each file
// inside it and nothing is moved into itself.
func moveRenamePaths(t *treeFS, root, newName string) error {
	moves, err := renamePathMoves(root, newName)
	if err != nil {
		return err
	}
	for _, m := range moves {
		oldAbs := filepath.Join(root, filepath.FromSlash(m[0]))
		newAbs := filepath.Join(root, filepath.FromSlash(m[1]))
		if err := t.Rename(oldAbs, newAbs); err != nil {
			return fmt.Errorf("move %s to %s: %w", m[0], m[1], err)
		}
	}
	return nil
}

func renamePathMoves(root, newName string) ([][2]string, error) {
	prefixes := map[string]struct{}{}
	if err := walkRenameTree(root, func(rel string, _ os.FileInfo) error {
		parts := strings.Split(rel, "/")
		for i, part := range parts {
			if strings.Contains(part, renameOldName) {
				prefixes[strings.Join(parts[:i+1], "/")] = struct{}{}
				break
			}
		}
		return nil
	}); err != nil {
		return nil, err
	}

	ordered := make([]string, 0, len(prefixes))
	for prefix := range prefixes {
		ordered = append(ordered, prefix)
	}
	slices.Sort(ordered)

	moves := make([][2]string, 0, len(ordered))
	for _, old := range ordered {
		nested := false
		for _, other := range ordered {
			if other != old && strings.HasPrefix(old, other+"/") {
				nested = true
				break
			}
		}
		if nested {
			continue
		}
		moves = append(moves, [2]string{old, strings.ReplaceAll(old, renameOldName, newName)})
	}
	return moves, nil
}

// repointRenameSymlinks rewrites a symlink's TARGET, which no content pass can
// see: `.agents/skills/<name>` is a tracked entry whose "content" is the target
// string. Moving the skill directory without this leaves the registration
// dangling and the skill silently invisible to an agent host.
func repointRenameSymlinks(t *treeFS, root, newName string) error {
	return walkRenameTree(root, func(rel string, info os.FileInfo) error {
		if info.Mode()&os.ModeSymlink == 0 {
			return nil
		}
		abs := filepath.Join(root, filepath.FromSlash(rel))
		target, err := t.Readlink(abs)
		if err != nil {
			return fmt.Errorf("read link %s: %w", rel, err)
		}
		fixed := strings.ReplaceAll(target, renameOldName, newName)
		if fixed == target {
			return nil
		}
		if err := t.Symlink(fixed, abs); err != nil {
			return fmt.Errorf("repoint %s: %w", rel, err)
		}
		return nil
	})
}

// walkRenameTree visits every entry under root - files, directories and
// symlinks - skipping .git. Slash-separated relative paths keep the comparison
// with the rename rules (written for git) the same on every platform. Walk does
// not follow symlinks, so a link is reported as a link, never as its target.
func walkRenameTree(root string, fn func(rel string, info os.FileInfo) error) error {
	return filepath.Walk(root, func(p string, info os.FileInfo, err error) error {
		if err != nil {
			return err
		}
		rel, relErr := filepath.Rel(root, p)
		if relErr != nil {
			return relErr
		}
		if rel == "." {
			return nil
		}
		rel = filepath.ToSlash(rel)
		if rel == ".git" || strings.HasPrefix(rel, ".git/") {
			if info.IsDir() {
				return filepath.SkipDir
			}
			return nil
		}
		// Generated by site/build.mjs: never hand-edited, so never moved
		// either. Same skip list as the content pass and as
		// scripts/rename-project.py's GENERATED_PREFIXES, so the two ports stay
		// interchangeable.
		if rel == "site/docs" || strings.HasPrefix(rel, "site/docs/") {
			if info.IsDir() {
				return filepath.SkipDir
			}
			return nil
		}
		if rel == "site/llms.txt" || rel == "site/llms-full.txt" || rel == "site/sitemap.xml" {
			return nil
		}
		return fn(rel, info)
	})
}

func collectRenameFiles(root string) ([]string, error) {
	var files []string
	err := filepath.Walk(root, func(p string, info os.FileInfo, err error) error {
		if err != nil {
			return err
		}
		rel, _ := filepath.Rel(root, p)
		if info.IsDir() {
			if rel == ".git" || strings.HasPrefix(rel, ".git"+string(filepath.Separator)) {
				return filepath.SkipDir
			}
			if rel == "site/docs" || strings.HasPrefix(rel, "site/docs"+string(filepath.Separator)) {
				return filepath.SkipDir
			}
			return nil
		}
		if rel == "site/llms.txt" || rel == "site/llms-full.txt" || rel == "site/sitemap.xml" {
			return nil
		}
		if rel == filepath.Join("cmd", "gogogo", "rename.go") {
			return nil
		}
		// Skip symlinks and other non-regular files. .agents/skills/*
		// are symlinks to dirs inside skills/; Walk reports the link
		// itself (IsDir false), and reading through it fails with
		// "is a directory". The link target is walked via its real
		// path, so nothing is missed by skipping the link.
		if !info.Mode().IsRegular() {
			return nil
		}
		files = append(files, p)
		return nil
	})
	return files, err
}

func rewriteRenameFile(
	t *treeFS,
	path, oldModule, newModule string,
	oldOwner, oldName, newOwner, newName string,
	foreign []string,
) error {
	raw, err := t.ReadFile(path)
	if err != nil {
		return fmt.Errorf("read %s: %w", path, err)
	}
	if len(raw) > 0 && isBinary(raw) {
		return nil
	}
	text := string(raw)
	orig := text
	placeholders := map[string]string{}
	for i, repo := range foreign {
		token := "\x00FOREIGN" + string(rune('0'+i)) + "\x00"
		if strings.Contains(text, repo) {
			placeholders[token] = repo
			text = strings.ReplaceAll(text, repo, token)
		}
	}
	text = strings.ReplaceAll(text, oldModule, newModule)
	if newOwner != oldOwner {
		text = strings.ReplaceAll(text, oldOwner+"/"+oldName, newOwner+"/"+newName)
		text = strings.ReplaceAll(text, oldOwner+".github.io", newOwner+".github.io")
	}
	text = strings.ReplaceAll(text, oldName, newName)
	for token, repo := range placeholders {
		text = strings.ReplaceAll(text, token, repo)
	}
	if text != orig {
		if err := t.WriteFile(path, []byte(text), scaffoldFileMode); err != nil {
			return err
		}
	}
	return nil
}

func isBinary(chunk []byte) bool {
	if len(chunk) > binarySniffLen {
		chunk = chunk[:binarySniffLen]
	}
	return slices.Contains(chunk, 0)
}
