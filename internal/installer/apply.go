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
	// printCheck already explained a non-template tree in one line; the
	// inventory message below would contradict it (neither "manifest drift"
	// nor "already trimmed" describes "wrong repository").
	if !looksLikeTemplate(opt.dir) {
		printCheck(stdout, nil, opt.dir)
		return &ExitError{code: 1, msg: "not a gogogo checkout (see CHECK-FAIL above)"}
	}
	if failed := printCheck(stdout, checkTree(opt.dir), opt.dir); failed > 0 {
		return &ExitError{code: 1, msg: fmt.Sprintf(
			"%d unit(s) cannot apply cleanly here — manifest drift "+
				"or already-trimmed tree (see CHECK-FAIL lines above)", failed)}
	}
	// Reaching here means every unit either applies or is recorded as
	// deliberately removed. Say which, so a trimmed tree does not read as a
	// silent pass.
	fmt.Fprintln(stdout, "gogogo: check OK — every non-trimmed unit applies "+
		"cleanly here (CHECK-TRIMMED lines are expected for a scaffolded tree)")
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
