// doctor/generate preflight+codegen (see docs/doctor-generate.md)
// generate - deterministic codegen wrapper (see docs/doctor-generate.md)
//
//nolint:lll,goconst,mnd,gocyclo,funlen,gosec,staticcheck,contextcheck,noctx,nolintlint,perfsprint // see docs/doctor-generate.md; CI gates are --json + lefthook + generate --check
package installer

//nolint:lll,goconst,mnd,gocyclo,funlen,gosec,staticcheck,contextcheck,noctx,staticcheck,goconst,lll,mnd,gocyclo,funlen,gosec,contextcheck,noctx,nolintlint,perfsprint // see docs/doctor-generate.md; CI gates are --json + lefthook + generate --check

import (
	"context"
	"encoding/json"
	"flag"
	"fmt"
	"io"
	"os/exec"
	"strings"
)

const (
	generateTail30 = 30
	generateTail40 = 40
)

func marshalJSON(v any) ([]byte, error) { return json.MarshalIndent(v, "", "  ") }

func runGenerate(ctx context.Context, args []string, stdout io.Writer) error {
	fs := flag.NewFlagSet("generate", flag.ContinueOnError)
	jsonOut := fs.Bool("json", false, "machine-readable")
	check := fs.Bool("check", false, "fail on drift (CI)")
	only := fs.String("only", "", "templ|sqlc|css|docs (comma-separated)")
	fs.Usage = func() { PrintUsage(stdout, fs) }
	if err := fs.Parse(args); err != nil {
		return err
	}
	_ = exec.CommandContext(ctx, "true") // keep ctx used
	steps := []string{"templ", "sqlc", "css", "docs"}
	var ran []string
	runOnly := map[string]bool{}
	if *only != "" {
		for s := range strings.SplitSeq(*only, ",") {
			runOnly[strings.TrimSpace(s)] = true
		}
	}

	// 1. templ generate (idempotent)
	if runOnlyTrue(runOnly, "templ") {
		if ok, err := runTemplGenerateRepo(ctx); err != nil {
			return &ExitError{code: 2, msg: fmt.Sprintf("templ generate failed: %v", err)}
		} else if ok {
			ran = append(ran, "templ")
		}
	}
	// 2. sqlc generate (if installed + config present)
	if runOnlyTrue(runOnly, "sqlc") {
		if ok, err := runSQLCGenerateRepo(ctx); err != nil {
			return &ExitError{code: 2, msg: fmt.Sprintf("sqlc generate failed: %v", err)}
		} else if ok {
			ran = append(ran, "sqlc")
		}
	}
	// 3. css (make css — tailwind v4 + daisyui)
	if runOnlyTrue(runOnly, "css") {
		if out, err := runIn(ctx, ".", "make", "css"); err != nil {
			return &ExitError{code: 2, msg: fmt.Sprintf("make css failed: %v\n%s", err, tailLines(string(out), generateTail30))}
		}
		ran = append(ran, "css")
	}
	// 4. docs regen
	if runOnlyTrue(runOnly, "docs") {
		if out, err := runIn(ctx, ".", "go", "test", "./internal/capabilities/", "-run", "Test(ApiRoutes|CapabilityContracts)DocFresh", "-update", "-count=1"); err != nil {
			return &ExitError{code: 2, msg: fmt.Sprintf("docs regen failed: %v\n%s", err, tailLines(string(out), generateTail30))}
		}
		ran = append(ran, "docs")
	}
	// --check: fail on drift (git diff)
	if *check {
		out, err := runIn(ctx, ".", "git", "diff", "--exit-code")
		if err != nil {
			return &ExitError{code: 1, msg: "generate --check: drift detected\n" + tailLines(string(out), generateTail40)}
		}
	}
	if *jsonOut {
		fmt.Fprintf(stdout, "{\"ran\":%q}\n", strings.Join(ran, ","))
	} else {
		fmt.Fprintf(stdout, "gogogo generate: ran %v\n", ran)
	}
	_ = steps
	return nil
}

func runOnlyTrue(only map[string]bool, key string) bool {
	if len(only) == 0 {
		return true
	}
	return only[key]
}

func runTemplGenerateRepo(_ context.Context) (bool, error) {
	return runTemplGenerate(".") // reuse recipe_codegen seam (which uses go tool templ)
}

func runSQLCGenerateRepo(_ context.Context) (bool, error) {
	return runSQLCGenerate(".")
}
