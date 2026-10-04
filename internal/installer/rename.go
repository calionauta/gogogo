// SCOPE:layer=infra,removal=plugin — installer engine: project rename (module path + references)
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

// renameTree ports the scripts/rename-project.py rules: module path first,
// bare name second, Pages host only on owner change. Shields sibling repos,
// skips generated site/docs output. Operates on tracked files when inside a
// git checkout, otherwise walks the tree skipping .git.
func renameTree(root, newName, newOwner string) error {
	const oldName = "gogogo"
	const oldOwner = "calionauta"
	oldModule := "github.com/" + oldOwner + "/" + oldName
	newModule := "github.com/" + newOwner + "/" + newName

	foreign := []string{
		"github.com/calionauta/ai-credits",
		"github.com/calionauta/datastar-lint",
		"github.com/calionauta/pi-leakguard",
	}

	files, err := collectRenameFiles(root)
	if err != nil {
		return err
	}

	for _, p := range files {
		if err := rewriteRenameFile(p, oldModule, newModule, oldOwner, oldName, newOwner, newName, foreign); err != nil {
			return err
		}
	}
	return nil
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
		files = append(files, p)
		return nil
	})
	return files, err
}

func rewriteRenameFile(
	path, oldModule, newModule string,
	oldOwner, oldName, newOwner, newName string,
	foreign []string,
) error {
	raw, err := os.ReadFile(path)
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
		//nolint:gosec // G306 scaffolded repo files are 0644 tracked sources, same as a git checkout.
		if err := os.WriteFile(path, []byte(text), scaffoldFileMode); err != nil {
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
