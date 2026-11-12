package main

// The CLI reference must name every verb the installer dispatches.
//
// Why: the docs gate already rejects a `gogogo <verb>` the CLI does NOT
// have (a doc that invents a command). The other direction was missing: a
// verb the CLI HAS and the reference page never mentions. That is how a
// command ships undocumented — the reference reads complete, the tool works,
// and the reader never learns the verb exists. #404 shipped `recipe test`
// and the reference learned about it by luck; this makes the luck
// unnecessary.
//
// The verbs are read from the dispatch switch, the same way the invented-
// command check reads it: the source is the truth, the page is the copy.

import (
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"regexp"
	"slices"
	"strings"
)

// recipeVerbRE extracts the recipe group's verbs from its dispatch switch.
// The alias case (`case "describe", "show":`) yields the canonical name
// only: the page documents one spelling, and the alias is the same verb.
var recipeVerbRE = regexp.MustCompile(`case "([a-z][a-z-]*)"`)

// cliReferencePage is the page that must carry every verb.
const cliReferencePage = "docs/cli.md"

// checkCLIVerbs rejects a verb the installer dispatches and the reference
// page never names.
func checkCLIVerbs(root string, topLevel, recipeVerbs []string) []string {
	lines, err := fileLines(root, cliReferencePage)
	if err != nil {
		return []string{cliReferencePage + ": " + err.Error()}
	}
	documented := map[string]bool{}
	for _, line := range lines {
		for _, tick := range tickRE.FindAllStringSubmatch(line, -1) {
			span := tick[1]
			fields := strings.Fields(span)
			for i, f := range fields {
				// The binary may be named by path: `go run ./cmd/gogogo recipe list`.
				if filepath.Base(f) != "gogogo" {
					continue
				}
				if i+1 < len(fields) {
					documented[fields[i+1]] = true
				}
				// `gogogo recipe <verb>` counts the verb, not the noun.
				if i+2 < len(fields) && fields[i+1] == "recipe" {
					documented["recipe "+fields[i+2]] = true
				}
			}
		}
	}
	var out []string
	for _, verb := range topLevel {
		if !documented[verb] {
			out = append(out, fmt.Sprintf(
				"%s: `gogogo %s` is a CLI verb the reference page never names", cliReferencePage, verb))
		}
	}
	for _, verb := range recipeVerbs {
		if !documented["recipe "+verb] {
			out = append(out, fmt.Sprintf(
				"%s: `gogogo recipe %s` is a recipe verb the reference page never names", cliReferencePage, verb))
		}
	}
	return out
}

// recipeVerbs reads the recipe group's dispatch switch.
func recipeVerbs(root string) ([]string, error) {
	raw, err := os.ReadFile(filepath.Join(root, "internal", "installer", "recipe_cli.go"))
	if err != nil {
		return nil, fmt.Errorf("read recipe dispatch: %w", err)
	}
	var out []string
	for _, m := range recipeVerbRE.FindAllStringSubmatch(string(raw), -1) {
		if !slices.Contains(out, m[1]) {
			out = append(out, m[1])
		}
	}
	if len(out) == 0 {
		return nil, errors.New("no recipe verbs found in internal/installer/recipe_cli.go — the dispatch parser broke")
	}
	slices.Sort(out)
	return out, nil
}
