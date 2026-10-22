// SCOPE:core — bump the pinned Go toolchain across every file that names it.
//
// Why this exists: the pin lives in every file that names the toolchain —
// go.mod, cmd/gogogo-mcp/go.mod, bin/install-go-toolchain.sh, both Dockerfiles,
// the coding-standards skill, docs/troubleshooting.md and the pin test's own
// table (surface.go lists all of them). Consistency between them is a TEST,
// not a script: internal/toolchain_pin_test.go catches a missed pin, but only
// AFTER the bump, when a human has to go and find what was missed. It caught
// one once: the #191 bump left deploy/Dockerfile.prod saying
// `golang:1.27-alpine` while go.mod declared `go 1.27.3`, so the production
// image could be built by a toolchain older than the tree requires.
//
// This tool is the other half: it does the bump in one command, so the pin
// never has to be moved by hand, and it FAILS LOUDLY when the file list and
// the tree disagree — a bump tool that silently skips a file is the same bug
// with a nicer name.
//
// It also closes the gap that made the CVE-2026-111943 bump a manual act.
// Nothing in the repo noticed that Go 1.27.3 existed until a developer pushed
// and the pre-push govulncheck failed on GO-2026-6809. A scheduled job that
// opens a reviewed PR when a newer patch of the current minor line ships
// (.github/workflows/go-auto-bump.yml) turns "someone noticed" into "the repo
// noticed" — the difference between a tree that is patched the same week and
// one that waits for the next push.
//
// Policy: PATCH only, within the current minor line. A minor bump (1.27 →
// 1.28) changes language semantics and the doc claims that speak at minor
// granularity, so it stays a human decision and this tool refuses it.
package main

import (
	"context"
	"errors"
	"flag"
	"fmt"
	"io"
	"os"
	"path/filepath"
	"strconv"
	"strings"
)

const (
	// goModPath is the file that carries the authoritative pin.
	goModPath = "go.mod"
)

func main() {
	if err := run(context.Background(), os.Args[1:], os.Stdout, os.Stderr); err != nil {
		fmt.Fprintf(os.Stderr, "bump-go-toolchain: %v\n", err)
		os.Exit(1)
	}
}

// opts is the parsed command line.
type opts struct {
	root   string
	to     string
	detect bool
	dryRun bool
}

// run is the whole tool, with its IO and context injected so the test can
// drive it.
func run(ctx context.Context, args []string, stdout, stderr io.Writer) error {
	return runWith(ctx, args, stdout, stderr, newOnline())
}

// runWith is run with the network edge injected, which is the only way to test
// the detect and "does this release exist" paths without go.dev.
func runWith(ctx context.Context, args []string, stdout, stderr io.Writer, net *online) error {
	o, err := parseOpts(args, stderr)
	if err != nil {
		return err
	}

	root, err := resolveRoot(o.root)
	if err != nil {
		return err
	}

	current, line, err := currentVersion(root)
	if err != nil {
		return err
	}

	if o.detect {
		return detectOnly(ctx, net, line, stdout, stderr)
	}

	target := o.to
	if target == "" {
		target, err = net.latestPatch(ctx, line)
		if err != nil {
			return err
		}
		if target == "" {
			fmt.Fprintf(stdout, "✓ already the newest patch of the %s line — nothing to do\n", line)
			return nil
		}
	}

	if target == current {
		fmt.Fprintf(stdout, "✓ already at %s — nothing to do\n", target)
		return nil
	}
	if err := checkTarget(current, line, target); err != nil {
		return err
	}

	// An explicit target is the only one this tool can be wrong about: the
	// detected one came out of the feed a moment ago. Confirm it exists BEFORE
	// applyBump, so a typo costs a request, not a tree pinned to a toolchain
	// that cannot be downloaded.
	if o.to != "" {
		if err := net.checkPublished(ctx, target); err != nil {
			return err
		}
	}

	return applyBump(root, current, target, o.dryRun, stdout)
}

// parseOpts reads the command line.
func parseOpts(args []string, stderr io.Writer) (opts, error) {
	var o opts
	fs := flag.NewFlagSet("bump-go-toolchain", flag.ContinueOnError)
	fs.SetOutput(stderr)
	fs.StringVar(&o.root, "root", "", "tree to rewrite (default: the repo above the working directory)")
	fs.StringVar(&o.to, "to", "", "target version, full X.Y.Z (default: the newest patch of the current "+
		"minor line). An explicit target is confirmed against go.dev/dl before anything is written, "+
		"so a version that was never published cannot be pinned.")
	fs.BoolVar(&o.detect, "detect", false, "print the target version and exit; nothing is written")
	fs.BoolVar(&o.dryRun, "dry-run", false, "report what would change; nothing is written")
	if err := fs.Parse(args); err != nil {
		return o, err
	}
	return o, nil
}

// resolveRoot finds the tree to rewrite.
func resolveRoot(root string) (string, error) {
	if root != "" {
		return root, nil
	}
	abs, err := findRepoRoot(".")
	if err != nil {
		return "", err
	}
	return abs, nil
}

// detectOnly prints the target version and writes nothing.
func detectOnly(ctx context.Context, net *online, line string, stdout, stderr io.Writer) error {
	target, err := net.latestPatch(ctx, line)
	if err != nil {
		return err
	}
	if target == "" {
		fmt.Fprintf(stderr, "no newer patch of the %s line is published — nothing to detect\n", line)
		return nil
	}
	fmt.Fprintln(stdout, target)
	return nil
}

// applyBump rewrites the surface and reports what changed.
func applyBump(root, current, target string, dryRun bool, stdout io.Writer) error {
	changed, err := bump(root, current, target, dryRun)
	if err != nil {
		return err
	}

	if dryRun {
		fmt.Fprintf(stdout, "→ %s → %s (dry run, nothing written)\n", current, target)
		for _, f := range changed {
			fmt.Fprintf(stdout, "  would change: %s\n", f)
		}
		return nil
	}

	if err := verify(root, current, target); err != nil {
		return err
	}

	fmt.Fprintf(stdout, "→ %s → %s\n", current, target)
	for _, f := range changed {
		fmt.Fprintf(stdout, "  changed: %s\n", f)
	}
	fmt.Fprintf(stdout, "✓ every file that names the toolchain now says %s\n", target)
	return nil
}

// findRepoRoot walks up from dir until it finds the go.mod that declares the
// module. A bump tool run from the wrong directory must not guess.
func findRepoRoot(dir string) (string, error) {
	abs, err := filepath.Abs(dir)
	if err != nil {
		return "", err
	}
	for {
		if _, err := os.Stat(filepath.Join(abs, goModPath)); err == nil {
			return abs, nil
		}
		parent := filepath.Dir(abs)
		if parent == abs {
			return "", errors.New("no go.mod above the working directory — run this from inside the repo, " +
				"or pass --root")
		}
		abs = parent
	}
}

// currentVersion reads the pinned version from go.mod and returns it with
// the release line it belongs to ("1.27").
func currentVersion(root string) (version, line string, err error) {
	body, err := os.ReadFile(filepath.Join(root, goModPath))
	if err != nil {
		return "", "", fmt.Errorf("read %s: %w", goModPath, err)
	}
	for raw := range strings.SplitSeq(string(body), "\n") {
		fields := strings.Fields(raw)
		if len(fields) != 2 || fields[0] != "go" {
			continue
		}
		major, minor, _, ok := parseVersion(fields[1])
		if !ok {
			return "", "", fmt.Errorf("%s declares %q, which is not a version I understand",
				goModPath, fields[1])
		}
		return fields[1], strconv.Itoa(major) + "." + strconv.Itoa(minor), nil
	}
	return "", "", fmt.Errorf("%s has no `go` directive", goModPath)
}

// checkTarget refuses a target that is not a patch of the current release
// line.
func checkTarget(current, line, target string) error {
	tmaj, tmin, tpatch, ok := parseVersion(target)
	if !ok {
		return fmt.Errorf("target %q is not a full version (X.Y.Z)", target)
	}
	cmaj, cmin, cpatch, _ := parseVersion(current)
	if tmaj != cmaj || tmin != cmin {
		return fmt.Errorf("target %s leaves the %s line the tree is on: a MINOR bump changes "+
			"language semantics and the doc claims that speak at minor granularity, so it is a "+
			"human decision, and this tool moves patch releases only", target, line)
	}
	if tpatch <= cpatch {
		return fmt.Errorf("target %s is not newer than the current %s — a bump never goes backwards",
			target, current)
	}
	return nil
}

// parseVersion splits "X.Y.Z" into its parts. The minor and patch parts are
// required: the pin is enforced at patch level, so a two-part version would
// be ambiguous.
func parseVersion(v string) (major, minor, patch int, ok bool) {
	parts := strings.Split(v, ".")
	if len(parts) != 3 {
		return 0, 0, 0, false
	}
	nums := make([]int, 3)
	for i, p := range parts {
		n, err := strconv.Atoi(p)
		if err != nil || n < 0 {
			return 0, 0, 0, false
		}
		nums[i] = n
	}
	return nums[0], nums[1], nums[2], true
}
