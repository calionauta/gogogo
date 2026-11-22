// Installer engine: project rename (module path + references)
package installer

import (
	"fmt"
	"os"
	"path/filepath"
	"regexp"
	"slices"
	"strings"

	"github.com/calionauta/gogogo/internal/recipe"
	shapekit "github.com/calionauta/gogogo/internal/shape"
)

// binarySniffLen bounds the binary sniff: NUL bytes past this window do not
// matter because the rewrite only touches text-like tracked sources.
const binarySniffLen = 8192

// renameIdentity answers "what is this tree called?" from the tree itself.
//
// It used to be three compile-time constants. That worked exactly once: a
// scaffold rewrites those constants, so the SECOND hop was correct only if the
// rewrite happened to catch every one of them — and it did not catch the owner,
// so a fork's tool cloned the template's org instead of its own. Reading
// `go.mod` makes the answer true for any tree, on any hop, without a constant
// that has to remember to rewrite itself.
//
// A tree with no readable module (a unit-test fixture) falls back to the
// template constants, which is what those fixtures assume.
func renameIdentity(root string) (owner, name, module string) {
	if raw, err := os.ReadFile(filepath.Join(root, "go.mod")); err == nil {
		for line := range strings.SplitSeq(string(raw), "\n") {
			mod, ok := strings.CutPrefix(strings.TrimSpace(line), "module ")
			if !ok {
				continue
			}
			if o, n, ok := splitModulePath(strings.TrimSpace(mod)); ok {
				return o, n, "github.com/" + o + "/" + n
			}
		}
	}
	return templateOwner, templateRepo, templateModulePath
}

// splitModulePath accepts `<host>/<owner>/<repo>` and nothing deeper: a test
// binary's main package is four segments (…/internal/installer), which is not a
// product identity.
func splitModulePath(mod string) (owner, repo string, ok bool) {
	parts := strings.Split(mod, "/")
	if len(parts) != 3 || !strings.Contains(parts[0], ".") {
		return "", "", false
	}
	return parts[1], parts[2], true
}

// renameTree ports the scripts/rename-project.py rules: module path first,
// bare name second, Pages host only on owner change, and then the paths.
// Shields sibling repos, skips generated site/docs output. Operates on tracked
// files when inside a git checkout, otherwise walks the tree skipping .git.
func renameTree(root, newName, newOwner string) error {
	oldOwner, oldName, oldModule := renameIdentity(root)
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
	if err := moveRenamePaths(t, root, oldName, newName); err != nil {
		return err
	}
	if err := repointRenameSymlinks(t, root, oldName, newName); err != nil {
		return err
	}
	// The receipts record a hash per copied file. Every rewrite above changed
	// those bytes, so a tree that skipped this step ships receipts whose hashes
	// no longer describe anything — and then `recipe install` copies wrong,
	// `--recipes` prunes wrong and `recipe test` derives the wrong scope. The
	// template's own receipts had rotted this way in 82 of 276 files.
	return rehashReceipts(root)
}

// rehashReceipts rewrites the recorded hashes of the files that still exist.
// A path the rename moved is not present any more; it is dropped from the
// receipt rather than recorded with a hash of nothing, because a receipt that
// claims a file nobody can open is the kind of lie `recipe receipts` exists to
// catch.
func rehashReceipts(root string) error {
	ids, err := recipe.ListReceipts(root)
	if err != nil {
		return err
	}
	for _, id := range ids {
		r, lerr := recipe.LoadReceipt(root, id)
		if lerr != nil {
			return lerr
		}
		if r.Removed != "" {
			continue // a tombstone describes data, not bytes
		}
		files := make(map[string]string, len(r.Files))
		for path := range r.Files {
			full := filepath.Join(root, filepath.FromSlash(path))
			raw, rerr := os.ReadFile(full)
			if rerr != nil {
				continue // moved away by the rename: the path is gone
			}
			files[path] = recipe.Hash(raw)
		}
		r.Files = files
		if err := recipe.SaveReceipt(root, r); err != nil {
			return err
		}
	}
	return nil
}

// moveRenamePaths moves every path whose NAME carries the template name:
// cmd/gogogo, cmd/gogogo-mcp and skills/gogogo-coding-standards. Only the
// shallowest such prefix moves, so a directory moves once instead of each file
// inside it and nothing is moved into itself.
func moveRenamePaths(t *treeFS, root, oldName, newName string) error {
	moves, err := renamePathMoves(root, oldName, newName)
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

func renamePathMoves(root, oldName, newName string) ([][2]string, error) {
	prefixes := map[string]struct{}{}
	if err := walkRenameTree(root, func(rel string, _ os.FileInfo) error {
		parts := strings.Split(rel, "/")
		for i, part := range parts {
			// A name-bearing path moves with the project. The consumer MCP is
			// the exception: its directory is a PLACEHOLDER (`product-mcp`), and
			// it must become the product's own name — two products on one PATH
			// cannot both install `product-mcp`.
			if part == shapekit.CmdProductMCPBase {
				prefixes[strings.Join(parts[:i+1], "/")] = struct{}{}
				break
			}
			if strings.Contains(part, oldName) {
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
		moves = append(moves, [2]string{old, renameTargetPath(old, oldName, newName)})
	}
	return moves, nil
}

// renameTargetPath is the new path for one move. A name-bearing path swaps the
// project name; the consumer MCP's placeholder segment becomes the product's
// name instead, because `product-mcp` contains nothing to substitute.
func renameTargetPath(old, oldName, newName string) string {
	if strings.HasSuffix(old, "/"+shapekit.CmdProductMCPBase) ||
		old == shapekit.CmdProductMCPBase {
		return strings.TrimSuffix(old, shapekit.CmdProductMCPBase) + newName + "-mcp"
	}
	return strings.ReplaceAll(old, oldName, newName)
}

// repointRenameSymlinks rewrites a symlink's TARGET, which no content pass can
// see: `.agents/skills/<name>` is a tracked entry whose "content" is the target
// string. Moving the skill directory without this leaves the registration
// dangling and the skill silently invisible to an agent host.
func repointRenameSymlinks(t *treeFS, root, oldName, newName string) error {
	return walkRenameTree(root, func(rel string, info os.FileInfo) error {
		if info.Mode()&os.ModeSymlink == 0 {
			return nil
		}
		abs := filepath.Join(root, filepath.FromSlash(rel))
		target, err := t.Readlink(abs)
		if err != nil {
			return fmt.Errorf("read link %s: %w", rel, err)
		}
		fixed := strings.ReplaceAll(target, oldName, newName)
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
// isVendoredSkill reports whether a slash-separated repo-relative path names
// a vendored upstream skill (skills/daisyui/, skills/stelow-workflow-
// coding-standards/). Both are byte-identical pins (UPSTREAM_SHA) refreshed
// by script: only skills/gogogo-coding-standards carries the project name,
// so only it moves or rewrites on a rename.
func isVendoredSkill(rel string) bool {
	return rel == "skills/daisyui" ||
		strings.HasPrefix(rel, "skills/daisyui/") ||
		strings.HasPrefix(rel, "skills/stelow-workflow-coding-standards")
}

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
		if rel == gitDirName || strings.HasPrefix(rel, gitDirName+"/") {
			if info.IsDir() {
				return filepath.SkipDir
			}
			return nil
		}
		// Generated by site/build.mjs: never hand-edited, so never moved
		// either. Same skip list as the content pass and as
		// scripts/rename-project.py's GENERATED_PREFIXES, so the two ports stay
		// interchangeable.
		if isVendoredSkill(rel) {
			if info.IsDir() {
				return filepath.SkipDir
			}
			return nil
		}
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
			if rel == gitDirName || strings.HasPrefix(rel, gitDirName+string(filepath.Separator)) {
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
		// Vendored upstream skills are byte-identical pins (UPSTREAM_SHA);
		// never rewrite them here. Dirs skip the subtree; files skip the write.
		if isVendoredSkill(filepath.ToSlash(rel)) {
			if info.IsDir() {
				return filepath.SkipDir
			}
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
		// The tool's own owner constant is what a FORK reads next: left alone,
		// a product's tool clones the template's org (`calionauta/<product>`)
		// instead of its own. The repo half follows the bare-name pass.
		text = ownerConstantRE(oldOwner).ReplaceAllString(text, "${1}"+newOwner+"${2}")
	}
	text = strings.ReplaceAll(text, oldName, newName)
	// The consumer MCP reports its own name to an MCP client, and it is a
	// PLACEHOLDER in this template (`Name: "product"`). Left alone, every
	// generated product would announce itself as "product" and an agent
	// connecting to two of them could not tell which is which.
	text = strings.ReplaceAll(text, `Name:    "product"`, `Name:    "`+newName+`"`)
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

// ownerConstantRE matches the tool's owner constant declaration, whatever the
// spacing, so the rewrite does not depend on how the file is formatted.
func ownerConstantRE(oldOwner string) *regexp.Regexp {
	return regexp.MustCompile(`(templateOwner\s*=\s*")` + regexp.QuoteMeta(oldOwner) + `(")`)
}

func isBinary(chunk []byte) bool {
	if len(chunk) > binarySniffLen {
		chunk = chunk[:binarySniffLen]
	}
	return slices.Contains(chunk, 0)
}
