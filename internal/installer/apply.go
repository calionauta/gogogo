// SCOPE:layer=infra,removal=plugin — installer engine: check gate and apply tail
package installer

import (
	"context"
	"fmt"
	"io"
)

// runCheck is the read-only drift gate: strict dir, no mutation, no tools.
func runCheck(opt options, stdout io.Writer) error {
	if err := requireCheckoutDir(opt); err != nil {
		return err
	}
	if failed := printCheck(stdout, checkTree(opt.dir)); failed > 0 {
		return &ExitError{code: 1, msg: fmt.Sprintf(
			"%d unit(s) cannot apply cleanly here — manifest drift "+
				"or already-trimmed tree (see CHECK-FAIL lines above)", failed)}
	}
	fmt.Fprintln(stdout, "gogogo: check OK — every unit applies cleanly here")
	return nil
}

// runApply is the mutating tail: preflight, clone-if-missing, confirm,
// then trim + prove. Separated so Run stays under the gocyclo gate.
func runApply(ctx context.Context, opt options, drop []trimUnit,
	plan scaffoldPlan, stdin io.Reader, stdout io.Writer,
) error {
	if err := guardMutable(opt.dir); err != nil {
		return err
	}
	if err := preflight(ctx, stdout, true, true, opt.format == planFormatJSON); err != nil {
		return err
	}
	if opt.run {
		if err := preflightRun(stdout); err != nil {
			return err
		}
	}
	if err := ensureCheckoutDir(ctx, opt, stdin, stdout); err != nil {
		return err
	}
	if !opt.yes && !confirm(stdout, stdin, len(drop)) {
		fmt.Fprintln(stdout, "gogogo: aborted — nothing changed (re-run with --yes to skip this prompt)")
		return nil
	}
	if opt.format == planFormatJSON {
		return applyAndProveJSON(ctx, opt, drop, stdout, plan)
	}
	return applyAndProve(ctx, opt, drop, stdout)
}
