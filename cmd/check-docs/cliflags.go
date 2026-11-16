package main

// The reference page must name every flag the CLI accepts.
//
// cliverbs.go catches a VERB the page never names. The flag half was missing:
// `--name` and `--owner` — the two arguments every scaffold takes — were absent
// from docs/cli.md while the page's examples showed `--shape` and `--recipes`.
// A reader had to learn them from `--help`.
//
// Flags are read from the registrations, the same way verbs are read from the
// dispatch switch: the source is the truth, the page is the copy.
//
// The rule is deliberately narrow. It does not judge WHERE the page mentions a
// flag (a table row, an example, a paragraph), only that the reference names it
// somewhere — a rule about prose would fail for the right sentence in the wrong
// place.

import (
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"regexp"
	"slices"
	"strings"
)

// installerFlagRE extracts a flag name from its registration. Only the typed
// entry points that take a name are matched (`fs.StringVar(&x, "name", …)`);
// `fs.Bool("no-tui", …)` style helpers name the flag in the first argument, and
// the tree does not use them.
var installerFlagRE = regexp.MustCompile(
	`fs\.(?:StringVar|BoolVar|IntVar|DurationVar)\(&[^,]+,\s*"([a-z][a-z0-9-]*)"`)

// checkCLIFlags rejects a flag the installer registers and the reference page
// never names.
func checkCLIFlags(root string) []string {
	flags, err := installerFlags(root)
	if err != nil {
		return []string{err.Error()}
	}
	raw, err := os.ReadFile(filepath.Join(root, cliReferencePage))
	if err != nil {
		return []string{cliReferencePage + ": " + err.Error()}
	}
	page := string(raw)
	var out []string
	for _, flag := range flags {
		if strings.Contains(page, "--"+flag) {
			continue
		}
		out = append(out, fmt.Sprintf(
			"%s: `--%s` is a CLI flag the reference page never names", cliReferencePage, flag))
	}
	return out
}

// installerFlags reads every flag registration under internal/installer.
func installerFlags(root string) ([]string, error) {
	paths, err := filepath.Glob(filepath.Join(root, "internal", "installer", "*.go"))
	if err != nil {
		return nil, fmt.Errorf("list installer sources: %w", err)
	}
	var out []string
	for _, path := range paths {
		raw, rerr := os.ReadFile(path)
		if rerr != nil {
			return nil, fmt.Errorf("read %s: %w", path, rerr)
		}
		for _, m := range installerFlagRE.FindAllStringSubmatch(string(raw), -1) {
			if !slices.Contains(out, m[1]) {
				out = append(out, m[1])
			}
		}
	}
	if len(out) == 0 {
		return nil, errors.New(
			"no flags found under internal/installer — the registration parser broke")
	}
	slices.Sort(out)
	return out, nil
}
