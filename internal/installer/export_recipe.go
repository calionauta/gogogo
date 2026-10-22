// SCOPE:plugin — installer engine: the port recipe a bundle carries
package installer

import (
	"fmt"
	"path/filepath"
	"strings"
)

// renderPortRecipe is the bundle's PORTING.md. It is deliberately a checklist,
// not a patch: only the human knows the router, the layout and the auth of the
// tree the unit is moving into.
func renderPortRecipe(b exportBundle) string {
	var sb strings.Builder
	module := b.Module
	if module == "" {
		module = "<your-module>"
	}
	fmt.Fprintf(&sb, "# Porting `%s` into your project\n\n", b.Unit)
	fmt.Fprintf(&sb, "Exported from `%s` into this directory. **Nothing outside this bundle "+
		"was written or changed** — this is a copy to apply deliberately, not an installer.\n\n", b.From)
	if b.Release != "" {
		fmt.Fprintf(&sb, "Snapshot of release **v%s** — if upstream moved on, re-export rather "+
			"than reading a newer recipe against older files.\n\n", b.Release)
	}
	sb.WriteString("`gogogo add` refuses a tree it did not scaffold, because it rewrites its " +
		"own router and capability registry. So the wiring is yours to write. The generic " +
		"checklist lives in the template's `docs/porting-into-an-existing-app.md`.\n\n")
	fmt.Fprintf(&sb, "Bundle units: %s.\n\n", strings.Join(b.Units, ", "))

	sb.WriteString("## 1. Move the files\n\n| In this bundle | Goes to (your repo) |\n|---|---|\n")
	for _, f := range b.Files {
		rel, err := filepath.Rel(b.To, f.To)
		if err != nil {
			rel = f.To
		}
		fmt.Fprintf(&sb, "| `%s` | `%s` |\n", filepath.ToSlash(rel), f.Dest)
	}
	sb.WriteString("\nKeep the paths repo-relative: the Go packages, the Templ `_templ.go` " +
		"files and the tests all assume them.\n\n")

	sb.WriteString("## 2. Rewrite the import path\n\n")
	if len(b.Rewrites) == 0 {
		fmt.Fprintf(&sb, "No copied file imports `%s` — nothing to rewrite.\n\n", templateModulePath)
	} else {
		renderRewriteSection(&sb, b, module)
	}

	sb.WriteString("## 3. Wire it (write these yourself)\n\n")
	if len(b.Routes) > 0 {
		sb.WriteString("Routes this unit registers — the path is yours to choose, the " +
			"handlers and the shapes they expect are not:\n\n")
		for _, line := range b.Routes {
			fmt.Fprintf(&sb, "    %s\n", line)
		}
		sb.WriteString("\n")
	}
	if len(b.Manual) == 0 {
		sb.WriteString("No anchored wiring lines were recorded for this unit. It still needs " +
			"a route and a constructor call wherever your app builds its handlers.\n\n")
	} else {
		for _, line := range b.Manual {
			fmt.Fprintf(&sb, "- `%s`\n", line)
		}
		sb.WriteString("\n")
	}
	sb.WriteString("Then the parts no marker can carry: mount the handler in your router, " +
		"add the sidebar entry (internal/ui/modules.go), and — if the handler reads or " +
		"writes user data — check the " +
		"session in the handler itself. In a gogogo tree that policy also lives in " +
		"`casbin/policy.csv`; your tree has its own answer.\n\n")

	proofHead := "## 5. Prove it"
	if len(b.FollowUps) > 0 {
		sb.WriteString("## 4. Follow-ups\n\n")
		for _, line := range b.FollowUps {
			fmt.Fprintf(&sb, "- %s\n", line)
		}
		sb.WriteString("\n")
	} else {
		proofHead = "## 4. Prove it"
	}
	fmt.Fprintf(&sb, "%s\n\nYour tree's gates, not the template's:\n\n", proofHead)
	for _, step := range b.Proof {
		fmt.Fprintf(&sb, "    %s\n", step)
	}
	sb.WriteString("\nThe template's own tests for this unit assume template fixtures; port " +
		"the ones that assert behaviour you keep, drop the ones that assert the template's mux.\n")
	return sb.String()
}

// renderRewriteSection is the import-rewrite half of the recipe.
func renderRewriteSection(sb *strings.Builder, b exportBundle, module string) {
	fmt.Fprintf(sb, "`%s` must become `%s` in these files:\n\n", templateModulePath, module)
	for _, f := range b.Rewrites {
		fmt.Fprintf(sb, "- `%s`\n", f.Dest)
	}
	fmt.Fprintf(sb, "\nIf you script it, review the diff afterwards:\n\n"+
		"    grep -rl '%s' . | xargs sed -i 's|%s|%s|g'\n\n",
		templateModulePath, templateModulePath, module)
	if b.Module == "" {
		sb.WriteString("`--module` was not given, so the placeholder above is yours to fill.\n\n")
	}
}
