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

// writeAgents writes the generated AGENTS.md for a drop list of builtin units,
// deriving each unit's paths from the registry instead of a `features/<id>`
// convention: a unit can own directories outside its feature folder
// (`whiteboard` also owns `internal/collab`), and a convention-derived list
// recorded only the feature directory — which made every doc mentioning the
// other one look like a dead path in a tree that had deliberately deleted it.
//
// ctx reaches the one subprocess this needs: reading the tree's HEAD commit.
func writeAgents(ctx context.Context, dir string, drop []builtinUnit) error {
	paths := droppedPaths(drop)
	if paths == nil {
		paths = []string{}
	}
	return writeAgentsWith(ctx, dir, "my-app", removedIDs(drop), paths)
}

// writeAgentsWith writes the generated AGENTS.md for an explicit removal set.
//
// It takes the ids AND paths directly rather than a []builtinUnit because the
// scaffold's demo slices (`--recipes`) are receipts dropped by id and have no
// builtinUnit to read paths from. Writing `removed: none`
// for them — which is what the first implementation did — makes the tree claim
// it is whole, so `--check` reports the dropped slices as manifest drift and the
// registry conformance test looks for directories the operator deliberately
// removed.
func writeAgentsWith(ctx context.Context, dir, projectName string, removedIDs, removedPaths []string) error {
	list := joinOrNone(removedIDs)
	paths := joinOrNone(removedPaths)
	out := prompts.MustRender("installer/agents-md", map[string]string{
		"project":         projectName,
		"toolchain":       toolchainLine(treeGoVersion(dir)),
		"template_ref":    templateRef(),
		"template_commit": gitHead(ctx, dir),
		"removed":         list,
		"removed_paths":   paths,
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
