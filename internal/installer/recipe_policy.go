// Install/uninstall own a recipe's RBAC rows.
//
// A recipe mounts routes the policy never saw: policy.csv is written before the
// recipe exists, and the runtime gate (rbac.RoutePolicyProblems) demands a
// `user` row for every route the composed tree serves. With none, an installed
// page answers 403 to every signed-in user in production — an install that
// "worked" and shipped a feature nobody can open.
//
// The rows are data, not code: policy.csv is a rule list the enforcer reads, and
// the block is marked with the recipe id so `recipe uninstall` removes exactly
// what it wrote and `recipe status` names its absence. The file stays the one
// policy source — a second, recipe-only policy stream would mean two answers to
// "may this role reach this path".
//
// Each row allows every verb (`.*`) for the same reason the core rows do: which
// verbs a pattern accepts is the router's business (chi answers 405), while the
// policy decides reachability.
package installer

import (
	"fmt"
	"io"
	"os"
	"slices"
	"sort"
	"strings"

	"github.com/calionauta/gogogo/internal/recipe"
)

// recipePolicyFile is the composed policy the enforcer is built from. It is
// embedded in the binary at build time, so the rows reach a running app the same
// way every other policy line does.
const recipePolicyFile = "internal/rbac/casbin/policy.csv"

// The markers bound one recipe's block. They are matched as whole trimmed lines,
// so `pomodoro` can never read `pomodoro-lab`'s block.
const (
	recipePolicyBegin = "# recipe:"
	recipePolicyEnd   = "# end recipe:"
)

// recipePolicyMode is the mode of the tracked policy file, kept explicit so a
// rewrite cannot silently change the permissions gitleaks and git expect.
const recipePolicyMode = 0o644

// recipePolicyOwner is the one subject a recipe's routes allow. A recipe cannot
// declare a role vocabulary — roles are core (internal/rbac/roles.go) — so its
// routes are reachable by every signed-in user and the handler does the
// per-record check, exactly like a built-in unit that keeps ownership in SQL.
const recipePolicyOwner = "user"

// recipePolicyRows renders the receipt's routes as policy rows, one per pattern,
// sorted. Two verbs on one pattern collapse into one row: the row allows `.*`.
// Chi placeholders ({docID}) become `*` (the Casbin object shape: `*` spans
// a whole segment, `{...}` is a rejected metacharacter).
func recipePolicyRows(d recipe.Declaration) []string {
	seen := make(map[string]bool, len(d.Routes))
	rows := make([]string, 0, len(d.Routes))
	for _, route := range d.Routes {
		if !strings.HasPrefix(route.Pattern, "/") || seen[route.Pattern] {
			continue
		}
		seen[route.Pattern] = true
		obj := route.Pattern
		for {
			start := strings.Index(obj, "{")
			if start < 0 {
				break
			}
			end := strings.Index(obj[start:], "}")
			if end < 0 {
				break
			}
			obj = obj[:start] + "*" + obj[start+end+1:]
		}
		rows = append(rows, fmt.Sprintf("p, %s, %s, .*", recipePolicyOwner, obj))
	}
	sort.Strings(rows)
	return rows
}

// assertRecipePolicyTarget checks the policy half of an install BEFORE the tree
// changes: a recipe with routes needs a file to put them in, and learning that
// after the files landed would leave a half-installed recipe behind.
//
// It also parses the existing block, so a hand-mangled one (unterminated, or two
// starters) stops the install instead of being silently rewritten.
func assertRecipePolicyTarget(root, id string, d recipe.Declaration) error {
	if len(recipePolicyRows(d)) == 0 {
		return nil
	}
	lines, exists, err := readRecipePolicy(root)
	if err != nil {
		return err
	}
	if !exists {
		return fmt.Errorf("recipe install: %s is missing from this checkout, "+
			"so the recipe's %d declared route(s) would have no policy row",
			recipePolicyFile, len(d.Routes))
	}
	if _, _, _, serr := splitRecipePolicy(lines, id); serr != nil {
		return serr
	}
	return nil
}

// syncRecipePolicy writes (or replaces, or removes) one recipe's block and
// returns the rows it left in the file.
//
// Rewriting only when the bytes differ keeps a re-install from dirtying a
// committed file, and the block replacement means an edited declaration takes
// effect without leaving the previous rows behind.
func syncRecipePolicy(root, id string, d recipe.Declaration) ([]string, error) {
	rows := recipePolicyRows(d)
	lines, exists, err := readRecipePolicy(root)
	if err != nil {
		return nil, err
	}
	if !exists {
		if len(rows) == 0 {
			return nil, nil
		}
		return nil, fmt.Errorf("recipe policy: %s is missing from this checkout", recipePolicyFile)
	}
	kept, _, _, serr := splitRecipePolicy(lines, id)
	if serr != nil {
		return nil, serr
	}
	if len(rows) == 0 {
		if len(kept) == len(lines) {
			return nil, nil
		}
		return nil, writeRecipePolicy(root, kept)
	}
	next := append(slices.Clone(kept), recipePolicyBlock(id, rows)...)
	if slices.Equal(next, lines) {
		return rows, nil
	}
	return rows, writeRecipePolicy(root, next)
}

// removeRecipePolicy deletes one recipe's block and reports whether there was
// one. A recipe with no routes is the normal case, so nothing is not an error.
func removeRecipePolicy(root, id string) (bool, error) {
	lines, exists, err := readRecipePolicy(root)
	if err != nil || !exists {
		return false, err
	}
	kept, _, found, serr := splitRecipePolicy(lines, id)
	if serr != nil {
		return false, serr
	}
	if !found {
		return false, nil
	}
	return true, writeRecipePolicy(root, kept)
}

// checkRecipePolicy compares the block with the declaration, the question
// `recipe status` asks of the mirror in another register: does the file still
// say what the code claims? It never writes — `recipe install` is the repair.
func checkRecipePolicy(root, id string, d recipe.Declaration) []string {
	want := recipePolicyRows(d)
	lines, exists, err := readRecipePolicy(root)
	if err != nil {
		return []string{err.Error()}
	}
	if !exists {
		return []string{recipePolicyFile + " is missing, so the recipe's routes have no policy row"}
	}
	_, block, found, serr := splitRecipePolicy(lines, id)
	if serr != nil {
		return []string{serr.Error()}
	}
	switch {
	case !found && len(want) == 0:
		return nil
	case !found:
		return []string{fmt.Sprintf(
			"%s has no block for %s: its routes answer 403 for every signed-in user "+
				"(re-run `gogogo recipe install %s`)", recipePolicyFile, id, id)}
	}
	got := make([]string, 0, len(block))
	for _, line := range block {
		if trimmed := strings.TrimSpace(line); trimmed != "" && !strings.HasPrefix(trimmed, "#") {
			got = append(got, trimmed)
		}
	}
	sort.Strings(got)
	if slices.Equal(got, want) {
		return nil
	}
	return []string{fmt.Sprintf(
		"%s block for %s does not match the declared routes: want %v, found %v "+
			"(re-run `gogogo recipe install %s`)", recipePolicyFile, id, want, got, id)}
}

// recipePolicyBlock is the text one recipe owns: a start marker, a line saying
// what owns it, the rows, and an end marker.
func recipePolicyBlock(id string, rows []string) []string {
	out := make([]string, 0, len(rows)+3)
	out = append(out, recipePolicyBegin+id,
		"# written by `gogogo recipe install`; `gogogo recipe uninstall` removes this block")
	out = append(out, rows...)
	return append(out, recipePolicyEnd+id)
}

// splitRecipePolicy returns the file without id's block, the block's own lines,
// and whether the block was there.
//
// A start marker with no matching end marker is an error rather than "eat the
// rest of the file": a truncated edit to a marked block would otherwise take
// every policy row after it with it, and the app would boot closed.
func splitRecipePolicy(lines []string, id string) (kept, block []string, found bool, err error) {
	begin, end := recipePolicyBegin+id, recipePolicyEnd+id
	start, stop := -1, -1
	for i, line := range lines {
		switch strings.TrimSpace(line) {
		case begin:
			if start >= 0 {
				return nil, nil, false, fmt.Errorf(
					"recipe policy: %s has two %q blocks for %s", recipePolicyFile, begin, id)
			}
			start = i
		case end:
			if start >= 0 && stop < 0 {
				stop = i
			}
		}
	}
	if start >= 0 && stop < 0 {
		return nil, nil, false, fmt.Errorf(
			"recipe policy: %s has an unterminated %q block for %s; add %q after the rows",
			recipePolicyFile, begin, id, end)
	}
	if start < 0 {
		return lines, nil, false, nil
	}
	kept = make([]string, 0, len(lines)-(stop-start+1))
	kept = append(kept, lines[:start]...)
	kept = append(kept, lines[stop+1:]...)
	return kept, lines[start : stop+1], true, nil
}

// readRecipePolicy returns the policy file's lines. A missing file is reported
// as absence, not an error: the caller distinguishes "not a checkout" (install
// refuses) from "block missing" (status reports).
func readRecipePolicy(root string) ([]string, bool, error) {
	rootFS, err := os.OpenRoot(root)
	if err != nil {
		return nil, false, fmt.Errorf("recipe policy: %w", err)
	}
	defer func() { _ = rootFS.Close() }()

	file, oerr := rootFS.Open(recipePolicyFile)
	if os.IsNotExist(oerr) {
		return nil, false, nil
	}
	if oerr != nil {
		return nil, false, fmt.Errorf("recipe policy: %w", oerr)
	}
	defer func() { _ = file.Close() }()

	body, rerr := io.ReadAll(file)
	if rerr != nil {
		return nil, false, fmt.Errorf("recipe policy: %w", rerr)
	}
	text := strings.TrimSuffix(string(body), "\n")
	if text == "" {
		return nil, true, nil
	}
	return strings.Split(text, "\n"), true, nil
}

// writeRecipePolicy replaces the file through an os.Root, so the path is fixed
// by this package and can never be redirected by a symlink in the tree.
func writeRecipePolicy(root string, lines []string) error {
	rootFS, err := os.OpenRoot(root)
	if err != nil {
		return fmt.Errorf("recipe policy: %w", err)
	}
	defer func() { _ = rootFS.Close() }()

	file, oerr := rootFS.OpenFile(recipePolicyFile, os.O_WRONLY|os.O_TRUNC, recipePolicyMode)
	if oerr != nil {
		return fmt.Errorf("recipe policy: %w", oerr)
	}
	body := strings.Join(lines, "\n") + "\n"
	if _, werr := file.WriteString(body); werr != nil {
		_ = file.Close()
		return fmt.Errorf("recipe policy: %w", werr)
	}
	if cerr := file.Close(); cerr != nil {
		return fmt.Errorf("recipe policy: %w", cerr)
	}
	return nil
}
