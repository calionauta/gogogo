// SCOPE:plugin — installer engine: upstream-first AGENTS.md generation
package installer

import (
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
NATS Leaf Node + idempotency), three UI skins, AI suggest + credits/BYOK
(GoAI + ai-credits), and admin surfaces. Do NOT add a dependency or a parallel
implementation before checking whether upstream already has it.

1. Fetch the map: https://calionauta.github.io/gogogo/llms.txt
2. Read the relevant page: https://calionauta.github.io/gogogo/docs/<slug>/
   (slugs: overview, getting-started, architecture, stack-layers, async-layers,
   features, scope-taxonomy, configuration, exception-to-go, todo-example, ui-skins, ui-sounds,
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

removed: __REMOVED__
removed-paths: __REMOVED_PATHS__

That removed: line is machine-read, not decoration: gogogo --check --dir . uses
it to tell a deliberately trimmed tree (CHECK-TRIMMED, exit 0) apart from real
manifest drift (CHECK-FAIL, exit 1). Keep it in sync if you remove units by
hand; a tree with no removed: line is checked strictly. The removed-paths:
line lists the repo-relative dirs and files those units owned (same
machine-read contract): the registry conformance tests skip exactly those
paths, so a trimmed tree stays green instead of reporting its own deliberate
removals as missing.
`

func writeAgents(dir, projectName string, drop []trimUnit) error {
	out := strings.ReplaceAll(agentsTemplate, "__PROJECT__", projectName)
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
