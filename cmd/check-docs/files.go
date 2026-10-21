// SCOPE:core — file discovery for the prose checks.

package main

import (
	"io/fs"
	"os"
	"path/filepath"
	"slices"
	"strings"
)

func docFiles(root string) ([]string, error) {
	var out []string
	for _, rel := range rootDocs {
		if _, err := os.Stat(filepath.Join(root, rel)); err == nil {
			out = append(out, rel)
		}
	}
	// docs/ is walked rather than globbed: filepath.Glob has no `**`, so a
	// nested page (docs/migrations/v0.44.0.md) escaped every prose rule while it
	// looked covered.
	docs, err := walkMarkdown(root, "docs")
	if err != nil {
		return nil, err
	}
	out = append(out, docs...)

	patterns := []string{"cmd/*/README.md", "skills/*/SKILL.md"}
	for _, pattern := range patterns {
		matches, err := filepath.Glob(filepath.Join(root, filepath.FromSlash(pattern)))
		if err != nil {
			return nil, err
		}
		for _, m := range matches {
			rel, relErr := filepath.Rel(root, m)
			if relErr != nil {
				return nil, relErr
			}
			rel = filepath.ToSlash(rel)
			// Vendored skills are upstream-owned (never edit them here).
			if strings.HasPrefix(rel, "skills/stelow-") {
				continue
			}
			out = append(out, rel)
		}
	}
	slices.Sort(out)
	return out, nil
}

// walkMarkdown lists every *.md under dir, repo-relative, at any depth.
func walkMarkdown(root, dir string) ([]string, error) {
	var out []string
	err := filepath.WalkDir(filepath.Join(root, filepath.FromSlash(dir)),
		func(path string, d fs.DirEntry, werr error) error {
			if werr != nil {
				return werr
			}
			if d.IsDir() || !strings.HasSuffix(path, ".md") {
				return nil
			}
			rel, relErr := filepath.Rel(root, path)
			if relErr != nil {
				return relErr
			}
			out = append(out, filepath.ToSlash(rel))
			return nil
		})
	return out, err
}
