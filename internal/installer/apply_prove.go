// Installer: the apply path — rename, prune, prove.
package installer

import (
	"context"
	"flag"
	"fmt"
	"io"
	"os"
	"os/exec"
	"sort"
	"strings"

	"github.com/calionauta/gogogo/internal/projectlocales"
)

// applyAndProve renames, writes AGENTS.md, and proves the result.
func applyAndProve(ctx context.Context, opt options, plan scaffoldPlan, stdout io.Writer) error {
	// ORDER: prune while the receipts still describe the tree, then rename (it
	// rewrites every file, including the registration the prune regenerates).
	if err := pruneReferenceRecipes(opt, plan.DropRecipes, stdout); err != nil {
		return err
	}
	// The language record lands BEFORE the proof: every later command
	// (new feature, recipe check) reads the operator's answer from here.
	if err := writeLocalesRecord(opt); err != nil {
		return err
	}
	// Prune and record BEFORE the rename, while every path still has the name
	// this binary was compiled against.
	//
	// The builder goes first: after the rename `cmd/gogogo-mcp` becomes
	// `cmd/<product>-mcp`, which is the consumer MCP's own new name, so the two
	// become indistinguishable. The shape record goes last of the three
	// because it is written to `.gogogo/shape.yml`, and the rename then moves
	// the directory to `.<product>/` and rewrites the entrypoint paths inside
	// it in step with the directory move. Recording after the rename recreates
	// `.gogogo/` and leaves the real tree without a record.
	if err := removeEvolutionPair(opt.dir); err != nil {
		return err
	}
	if err := applyShapeIfNeeded(opt, stdout); err != nil {
		return err
	}
	if err := renameTree(opt.dir, opt.name, opt.owner); err != nil {
		return err
	}
	if err := writeAgentsForPlan(ctx, opt.dir, opt.name, plan); err != nil {
		return err
	}
	if err := prove(ctx, opt.dir, stdout); err != nil {
		return err
	}
	printNextSteps(stdout, opt.dir, nil)
	if opt.run {
		return handoff(opt.dir, stdout)
	}
	return nil
}

// applyAndProveJSON is the machine-readable apply path: one envelope at
// the end carrying plan + receipt + proof result.
// writeAgentsForPlan writes the provenance for what this scaffold actually
// removed: the demo slices pruned by `--recipes`, plus the shape's entrypoints.
// A tree that dropped six slices and says `removed: none` is a lie the gates
// then act on — `--check` reports the (deliberate) absences as drift, and the
// registry conformance test looks for directories nobody meant to keep.
func writeAgentsForPlan(ctx context.Context, dir, projectName string, plan scaffoldPlan) error {
	ids := append([]string(nil), plan.DropRecipes...)
	paths := make([]string, 0, len(ids)*2)
	for _, id := range ids {
		paths = append(paths, "features/"+id)
	}
	// The shape's dropped entrypoints are removals too, and the same
	// conformance/`--check` rules read them.
	paths = append(paths, plan.ShapeRemove...)
	sort.Strings(ids)
	sort.Strings(paths)
	return writeAgentsWith(ctx, dir, projectName, ids, paths)
}

// writeLocalesRecord records the operator's language answer in the new tree. An
// empty answer writes nothing: the template's own record (or its default) stays,
// because the scaffold must not invent a decision the operator did not take.
func writeLocalesRecord(opt options) error {
	if strings.TrimSpace(opt.locales) == "" {
		return nil
	}
	if _, err := projectlocales.Write(opt.dir, splitIDs(opt.locales)); err != nil {
		return fmt.Errorf("scaffold: recording locales: %w", err)
	}
	return nil
}

func applyAndProveJSON(ctx context.Context, opt options, stdout io.Writer, plan scaffoldPlan) error {
	if err := pruneReferenceRecipes(opt, plan.DropRecipes, io.Discard); err != nil {
		return err
	}
	// Same order as applyAndProve: builder, shape record, then the rename.
	if err := removeEvolutionPair(opt.dir); err != nil {
		return err
	}
	if err := applyShapeIfNeeded(opt, io.Discard); err != nil {
		return err
	}
	if err := renameTree(opt.dir, opt.name, opt.owner); err != nil {
		return err
	}
	if err := writeAgents(ctx, opt.dir, opt.name, nil); err != nil {
		return err
	}
	if err := pruneReferenceRecipes(opt, plan.DropRecipes, io.Discard); err != nil {
		return err
	}
	// Silence prove chatter in JSON mode: discard its progress lines,
	// keep only the outcome for the envelope.
	devNull, err := os.OpenFile(os.DevNull, os.O_WRONLY, 0)
	if err != nil {
		return err
	}
	defer func() { _ = devNull.Close() }()
	proveErr := prove(ctx, opt.dir, devNull)
	env := envelope{Plan: plan, BuildOk: proveErr == nil, Next: buildNextSteps(opt.dir, nil)}
	if proveErr != nil {
		env.BuildErr = proveErr.Error()
	}
	if err := printEnvelopeJSON(stdout, env); err != nil {
		return err
	}
	if proveErr != nil {
		return proveErr
	}
	if opt.run {
		return handoff(opt.dir, stdout)
	}
	return nil
}

func confirm(stdout io.Writer, stdin io.Reader, nDrop int) bool {
	if nDrop == 0 {
		fmt.Fprint(stdout, "Proceed with rename only? [y/N]: ")
	} else {
		fmt.Fprintf(stdout, "Scaffold the full template into %s and rename? [y/N]: ", "the target directory")
	}
	var answer [8]byte
	n, _ := stdin.Read(answer[:])
	resp := strings.ToLower(strings.TrimSpace(string(answer[:n])))
	return resp == "y" || resp == "yes"
}

// plan output format names.
const (
	planFormatText = "text"
	planFormatJSON = "json"
)

// prove output tails kept short for humans; full logs stay in the checkout.
const (
	proofTemplTailLines = 10
	proofTidyTailLines  = 20
	proofBuildTailLines = 30
)

// prove runs templ generate (only when .templ files were edited in place),
// go mod tidy, and the build. A failing proof exits 2 with compiler output.
func prove(ctx context.Context, dir string, stdout io.Writer) error {
	// With no removal pass, no in-place .templ is edited: the rename rewrites only
	// module paths in Go sources, which `go build` proves as-is. The
	// templ step the old port path ran for edited .templ files is gone
	// with the removal pass itself.
	fmt.Fprintln(stdout, "gogogo: prove: go mod tidy …")
	if out, err := runIn(ctx, dir, "go", "mod", "tidy"); err != nil {
		return &ExitError{code: 2, msg: fmt.Sprintf(
			"go mod tidy failed (%v):\n%s", err, tailLines(string(out), proofTidyTailLines))}
	}
	// Regenerate the generated docs (route inventory + unit contract cards)
	// so the tree is green immediately.
	fmt.Fprintln(stdout, "gogogo: prove: generated docs …")
	// Warn-only here (failure is a stale doc, not a broken tree); the gate
	// below treats the same error as fatal.
	_ = proveAPIRoutesDoc(ctx, dir, stdout)
	fmt.Fprintln(stdout, "gogogo: prove: go build ./cmd/web …")
	tmp, err := os.CreateTemp("", "gogogo-proof-*")
	if err != nil {
		return err
	}
	tmpPath := tmp.Name()
	_ = tmp.Close()
	defer os.Remove(tmpPath)
	if out, err := runIn(ctx, dir, "go", "build", "-o", tmpPath, "./cmd/web"); err != nil {
		return &ExitError{code: 2, msg: fmt.Sprintf(
			"proof build failed (%v) — the rename is applied but broken:\n%s",
			err, tailLines(string(out), proofBuildTailLines))}
	}
	fmt.Fprintln(stdout, "gogogo: prove: build OK")
	return proveToolchainPin(ctx, dir, stdout)
}

func runIn(ctx context.Context, dir, name string, args ...string) ([]byte, error) {
	cmd := exec.CommandContext(ctx, name, args...)
	cmd.Dir = dir
	return cmd.CombinedOutput()
}

// proveAPIRoutesDoc regenerates the generated docs in the target tree: the
// route inventory and the unit contract cards. Both are pinned by tests, so a
// removal that deletes a unit without regenerating them leaves CI red.
// It returns the regen error (callers decide warn-only vs fatal) after
// printing the WARN with the manual regen command, so no failure mode is
// silent. Shared with the preset composition gate, which proves it on
// copies.
func proveAPIRoutesDoc(ctx context.Context, dir string, stdout io.Writer) error {
	if out, err := runIn(ctx, dir, "go", "test", "./internal/capabilities/",
		"-run", "Test(ApiRoutes|CapabilityContracts)DocFresh", "-update", "-count=1"); err != nil {
		fmt.Fprintf(stdout,
			"gogogo: WARN: generated docs regen failed (%v) — run "+
				"`go test ./internal/capabilities/ -run 'Test(ApiRoutes|CapabilityContracts)DocFresh' -update` manually:\n%s\n",
			err, tailLines(string(out), proofTidyTailLines))
		return err
	}
	return nil
}

func tailLines(s string, n int) string {
	lines := strings.Split(strings.TrimRight(s, "\n"), "\n")
	if len(lines) > n {
		lines = lines[len(lines)-n:]
	}
	return strings.Join(lines, "\n")
}

// UnitView is one installer unit for display (matrix, menus, docs).
type UnitView struct {
	ID      string
	Kind    string
	Summary string
}

// Units lists installer units in manifest order.
func Units() []UnitView {
	out := make([]UnitView, 0, len(builtinUnits))
	for _, u := range builtinUnits {
		m := u.meta()
		summary := u.id
		if s := unitOneLiner(u); s != u.id {
			summary = s
		}
		out = append(out, UnitView{ID: u.id, Kind: string(m.kind), Summary: summary})
	}
	return out
}

// flagGiven reports whether a flag was passed explicitly. Split out of
// loadOptions because the closure pushed that function over the gocyclo gate,
// and the distinction ("--dir was named" vs "the default") is what keeps
// --check-update from inventing a target directory.
func flagGiven(fs *flag.FlagSet, name string) bool {
	given := false
	fs.Visit(func(f *flag.Flag) {
		if f.Name == name {
			given = true
		}
	})
	return given
}
