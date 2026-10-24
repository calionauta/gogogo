// SCOPE:plugin — installer engine: upstream-first AGENTS.md generation
package installer

import (
	"context"
	"path/filepath"
	"strings"
)

// agentsTemplate is the AGENTS.md section every scaffolded project gets. It
// forces agents to check the upstream template before inventing a parallel
// implementation. __PROJECT__ is replaced with the new project name.
const agentsTemplate = `# AGENTS — upstream-first rule

This project (__PROJECT__) was scaffolded from
[calionauta/gogogo](https://github.com/calionauta/gogogo).

## Before creating a feature or installing a library, check upstream first

The template already ships: auth (owned argon2id + sessions), todo + realtime, background
jobs + retry (goqite + retry-go), durable workflows (DagNats over JetStream),
collaborative whiteboard (Loro CRDT), hybrid offline sync (Service Worker +
NATS Leaf Node + idempotency), DaisyUI + themes, AI suggest + credits/BYOK
(GoAI + ai-credits), and admin surfaces. Do NOT add a dependency or a parallel
implementation before checking whether upstream already has it.

1. Fetch the map: https://calionauta.github.io/gogogo/llms.txt
2. Read the relevant page: https://calionauta.github.io/gogogo/docs/<slug>/
   (slugs: overview, getting-started, architecture, stack-layers, async-layers,
   features, scope-taxonomy, configuration, exception-to-go, todo-example, ui-themes, ui-sounds,
   deploy, desktop-mobile, admin-dashboard, llm-and-credits, local-ci,
   code-quality, troubleshooting)
3. For non-docs files, read the upstream blob at the same path:
   https://github.com/calionauta/gogogo/blob/master/<path>
4. Reuse the upstream pattern (Todo is the reference implementation; SCOPE
   annotations say what is safe to delete). Only add a new library when no
   upstream page covers the need — and document why in the commit message.

## Trim provenance

__PROJECT__ was scaffolded with cmd/gogogo (see the template's
cmd/gogogo/README.md for the alternatives analysis). Removed units are gone
from disk with their wiring calls — not disabled behind flags. To remove more
later, follow docs/scope-taxonomy.md#removing-a-component and prove with
go build ./cmd/web.

scaffolded-from: __TEMPLATE_REF__
template-commit: __TEMPLATE_COMMIT__
removed: __REMOVED__
removed-paths: __REMOVED_PATHS__

Those four lines are machine-read, not decoration. The scaffolded-from: line
records the template revision this tree came from (and template-commit: the
commit it resolved to), which is what makes gogogo --check-update --dir . able
to say whether THIS project is behind upstream, instead of only whether the
installed tool is. The removed: line is what gogogo --check --dir . reads it
to tell a deliberately trimmed tree (CHECK-TRIMMED, exit 0) apart
from real manifest drift (CHECK-FAIL, exit 1). Keep it in sync if you remove units by
hand; a tree with no removed: line is checked strictly. The removed-paths:
line lists the repo-relative dirs and files those units owned (same
machine-read contract): the registry conformance tests skip exactly those
paths, so a trimmed tree stays green instead of reporting its own deliberate
removals as missing.

## Keeping this project current

The scaffolded-from: line above is the base this tree came from. Two read-only
commands compare it, and neither changes anything:

    gogogo --check-update --dir .   # tool and project vs the latest release
    gogogo migrate --dir .          # what changed between here and latest

--check-update exits 3 when an update exists. That is a VERDICT, not a failure:
do not treat its non-zero status as an error, and do not wrap it in set -e
without handling 3. Then apply what migrate printed, run the gates it names,
and bump scaffolded-from: to the revision you actually landed on.

Updating stays a human decision: re-run install.sh for the tool, and treat
migrate output as a list to apply deliberately. The template never merges into
this tree — you own every line of it.

When the request is "keep this current", "add unit X" or "add a feature",
gogogo advise --intent update|add-unit|new-feature --dir . prints the exact
command sequence for this tree, instead of you reconstructing it from the docs.
`

// writeAgents writes the generated AGENTS.md, including the machine-read
// provenance lines --check, --check-update and migrate all parse.
//
// ctx reaches the one subprocess this needs: reading the tree's HEAD commit.
// That commits the base marker to a real revision instead of a guess, and it
// costs one git call at scaffold time (skipped silently outside a git tree).
func writeAgents(ctx context.Context, dir, projectName string, drop []trimUnit) error {
	out := strings.ReplaceAll(agentsTemplate, "__PROJECT__", projectName)
	out = strings.ReplaceAll(out, "__TEMPLATE_REF__", templateRef())
	out = strings.ReplaceAll(out, "__TEMPLATE_COMMIT__", gitHead(ctx, dir))
	// "none" rather than an empty string, so the lines are always parseable
	// and always present — an empty value would read as a missing section.
	removed := removedIDs(drop)
	list := keepNone
	if len(removed) > 0 {
		list = strings.Join(removed, ", ")
	}
	out = strings.ReplaceAll(out, "__REMOVED__", list)
	out = strings.ReplaceAll(out, "__REMOVED_PATHS__", removedPaths(drop))
	t, err := openTree(dir)
	if err != nil {
		return err
	}
	defer t.Close()
	return t.WriteFile(filepath.Join(dir, "AGENTS.md"), []byte(out), scaffoldFileMode)
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
