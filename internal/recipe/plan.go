// SCOPE:plugin — Spike 4: install and update are one mechanism.
//
// The claim under test: install is just update against an empty tree, and update
// after user edits is decidable from the receipt alone — no merge engine, no
// version arithmetic. Plan computes file operations; the caller executes them.
//
// The three-way information this relies on is exactly what the receipt stores:
// the PRISTINE content hash (what install wrote), the CURRENT tree (what the user
// did), and the NEW UPSTREAM content (what upstream has now). Two of those are
// free; the third is the receipt's job, and its absence is why shadcn's diff is
// only two-way.
package recipe

import (
	"fmt"
	"sort"
	"strings"
)

// OpKind is what to do with one file.
type OpKind string

const (
	// OpWrite writes new content. Used for a file that does not exist yet (a fresh
	// install, or a file upstream added) and for a pristine file whose upstream
	// moved — both are safe because nothing the user wrote is at stake.
	OpWrite OpKind = "write"
	// OpFastForward replaces a pristine file with new upstream content. Distinct
	// from OpWrite only so the report can say "you did not touch this".
	OpFastForward OpKind = "fast-forward"
	// OpKeep leaves the file alone: the user modified it and upstream did not.
	OpKeep OpKind = "keep"
	// OpConflict means the user modified it AND upstream changed it. The planner
	// refuses to guess: it names the file and stops. Resolving is a real three-way
	// merge, which belongs to git, not to this package.
	OpConflict OpKind = "conflict"
	// OpObsolete means upstream dropped a file the user never touched. Deleting it
	// is safe; a file the user DID touch is OpKeep instead, because their bytes win.
	OpObsolete OpKind = "obsolete"
	// OpGone means upstream dropped a file the user had already deleted. Nothing to do.
	OpGone OpKind = "gone"
)

// Op is one planned filesystem action.
type Op struct {
	Kind     OpKind
	Path     string
	Content  string // set for OpWrite and OpFastForward
	userEdit bool   // internal: whether the user's bytes differ from pristine
}

// PlanResult is the whole answer for an install or an update.
type PlanResult struct {
	Ops       []Op
	Conflicts []string
}

// Blocked reports whether the plan cannot be applied without human resolution.
func (p PlanResult) Blocked() bool { return len(p.Conflicts) > 0 }

// Counts summarises the plan.
func (p PlanResult) Counts() (write, ff, keep, obsolete int) {
	for _, op := range p.Ops {
		switch op.Kind {
		case OpWrite:
			write++
		case OpFastForward:
			ff++
		case OpKeep:
			keep++
		case OpObsolete:
			obsolete++
		}
	}
	return write, ff, keep, obsolete
}

// Plan decides what to do for every file.
//
// upstream is path → new upstream content (the pristine bytes of the version being
// installed). r is the receipt of what is currently installed, or a zero receipt
// for a fresh install. root is the tree, read through current, which lets a caller
// answer "what does the user have" without this function touching the filesystem.
//
// The decision table, complete:
//
//	                | upstream has it              | upstream dropped it
//	not installed   | write                        | —
//	pristine        | fast-forward (or keep if same)| obsolete
//	modified        | CONFLICT (or keep if same)    | keep (their bytes win)
//	deleted         | write (their deletion was of an old file) | gone
func Plan(r Receipt, upstream map[string]string, current func(path string) (string, bool)) PlanResult {
	var out PlanResult
	for _, p := range planPaths(upstream, r.Files) {
		op, emit, conflict := decideOp(p, upstream, r.Files, current)
		if !emit {
			continue
		}
		out.Ops = append(out.Ops, op)
		if conflict {
			out.Conflicts = append(out.Conflicts, p)
		}
	}
	return out
}

// planPaths is the union of what upstream ships and what the receipt claims,
// sorted so a plan renders deterministically.
func planPaths(upstream map[string]string, installed map[string]string) []string {
	paths := make([]string, 0, len(upstream)+len(installed))
	seen := make(map[string]bool, len(upstream))
	for p := range upstream {
		paths = append(paths, p)
		seen[p] = true
	}
	for p := range installed {
		if !seen[p] {
			paths = append(paths, p)
		}
	}
	sort.Strings(paths)
	return paths
}

// decideOp applies the decision table documented on Plan to one path.
func decideOp(
	path string,
	upstream map[string]string,
	installed map[string]string,
	current func(path string) (string, bool),
) (op Op, emit, conflict bool) {
	up, inUpstream := upstream[path]
	pristineHash, wasInstalled := installed[path]
	cur, exists := current(path)

	switch {
	case !wasInstalled && inUpstream:
		return Op{Kind: OpWrite, Path: path, Content: up}, true, false

	case wasInstalled && exists:
		return decideInstalled(path, cur, up, pristineHash, inUpstream)

	case wasInstalled && !exists:
		// The user deleted it. If upstream still has it, re-writing would
		// resurrect a deliberate removal; but the file was DELETED, and the new
		// upstream version is not what they deleted. Report it rather than guess:
		// treat as keep, and let the operator decide to reinstall.
		return Op{Kind: OpGone, Path: path}, true, false

	default:
		// cannot happen (paths came from the union), guarded for completeness
		return Op{}, false, false
	}
}

// decideInstalled covers the three-way case: a file the receipt claims and the
// tree still has. Its whole question is whether the user's bytes moved and
// whether upstream's bytes moved.
func decideInstalled(path, cur, up, pristineHash string, inUpstream bool) (op Op, emit, conflict bool) {
	userEdited := Hash([]byte(cur)) != pristineHash
	upstreamMoved := !inUpstream || Hash([]byte(up)) != pristineHash
	switch {
	case userEdited && upstreamMoved:
		return Op{Kind: OpConflict, Path: path, userEdit: true}, true, true
	case userEdited && !upstreamMoved:
		return Op{Kind: OpKeep, Path: path, userEdit: true}, true, false
	case !userEdited && !inUpstream:
		return Op{Kind: OpObsolete, Path: path}, true, false
	case !userEdited && upstreamMoved:
		return Op{Kind: OpFastForward, Path: path, Content: up}, true, false
	default:
		// pristine and unchanged upstream: nothing to do, but reporting it
		// keeps the plan a complete picture of the file set
		return Op{Kind: OpKeep, Path: path}, true, false
	}
}

// Render writes the human plan.
func (p PlanResult) Render(w *strings.Builder) {
	write, ff, keep, obsolete := p.Counts()
	fmt.Fprintf(w, "plan: %d write, %d fast-forward, %d keep, %d obsolete, %d conflict\n",
		write, ff, keep, obsolete, len(p.Conflicts))
	for _, op := range p.Ops {
		switch op.Kind {
		case OpWrite:
			fmt.Fprintf(w, "  + %s (new)\n", op.Path)
		case OpFastForward:
			fmt.Fprintf(w, "  ^ %s (you did not touch it)\n", op.Path)
		case OpKeep:
			if op.userEdit {
				fmt.Fprintf(w, "  = %s (yours)\n", op.Path)
			}
		case OpObsolete:
			fmt.Fprintf(w, "  - %s (no longer upstream, untouched)\n", op.Path)
		case OpConflict:
			fmt.Fprintf(w, "  ! %s (yours AND upstream — needs a merge)\n", op.Path)
		}
	}
	if p.Blocked() {
		w.WriteString("  refusing to apply: resolve the conflicts above\n")
	}
}
