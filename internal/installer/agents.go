// Installer engine: upstream-first AGENTS.md generation
package installer

import (
	"context"
	"os"
	"path/filepath"
	"regexp"
	"strings"

	"github.com/calionauta/gogogo/internal/prompts"
)

// writeAgents writes the generated AGENTS.md, including the machine-read
// provenance lines --check, --check-update and migrate all parse.
//
// ctx reaches the one subprocess this needs: reading the tree's HEAD commit.
// That commits the base marker to a real revision instead of a guess, and it
// costs one git call at scaffold time (skipped silently outside a git tree).
func writeAgents(ctx context.Context, dir, projectName string, drop []builtinUnit) error {
	// "none" rather than an empty string, so the lines are always parseable
	// and always present — an empty value would read as a missing section.
	removed := removedIDs(drop)
	list := keepNone
	if len(removed) > 0 {
		list = strings.Join(removed, ", ")
	}
	out := prompts.MustRender("installer/agents-md", map[string]string{
		"project":         projectName,
		"toolchain":       toolchainLine(treeGoVersion(dir)),
		"template_ref":    templateRef(),
		"template_commit": gitHead(ctx, dir),
		"removed":         list,
		"removed_paths":   removedPaths(drop),
	})
	t, err := openTree(dir)
	if err != nil {
		return err
	}
	defer t.Close()
	return t.WriteFile(filepath.Join(dir, "AGENTS.md"), []byte(out), scaffoldFileMode)
}

// goDirectiveRE captures the `go 1.27.3` line of a go.mod.
var goDirectiveRE = regexp.MustCompile(`(?m)^go (\d+\.\d+(?:\.\d+)?)$`)

// treeGoVersion reads the Go version the target tree's go.mod pins, or "" when
// there is nothing to read (a synthetic fixture, a tree mid-rename).
//
// It is read from the target, not from this binary, because the proof runs
// INSIDE that tree: the version the scaffolded go.mod declares is the one its
// own pin test will demand.
func treeGoVersion(dir string) string {
	raw, err := os.ReadFile(filepath.Join(dir, "go.mod"))
	if err != nil {
		return ""
	}
	if m := goDirectiveRE.FindSubmatch(raw); m != nil {
		return string(m[1])
	}
	return ""
}

// toolchainLine is the sentence the generated AGENTS.md opens its Toolchain
// section with. Without a version it still says where the pin lives, because a
// scaffold must not fail over a line it cannot know.
func toolchainLine(version string) string {
	if version == "" {
		return "This tree builds with the Go version `go.mod` declares"
	}
	return "This tree builds with Go " + version + " — the pin `go.mod` declares"
}

// templateRef is the recorded base reference: "owner/repo@tag", or
// "owner/repo@dev" for a binary built without a release tag (go run, plain
// go build). The tag keeps its v prefix because that is what the upstream tag
// and the raw-file URLs actually are; comparison strips it.
func templateRef() string {
	tag := strings.TrimSpace(Version)
	switch tag {
	case "", "dev":
		tag = "dev"
	default:
		if !strings.HasPrefix(tag, "v") {
			tag = "v" + tag
		}
	}
	return templateOwner + "/" + templateRepo + "@" + tag
}

// gitHead reads the tree's HEAD commit, or "unknown" when the tree is not a
// git checkout (a plain copy, a tarball). "unknown" is written rather than the
// line being omitted, so the marker stays parseable and says exactly what it
// knows.
func gitHead(ctx context.Context, dir string) string {
	out, err := runIn(ctx, dir, "git", "rev-parse", "HEAD")
	if err != nil {
		return markerUnknown
	}
	head := strings.TrimSpace(string(out))
	if head == "" {
		return markerUnknown
	}
	return head
}
