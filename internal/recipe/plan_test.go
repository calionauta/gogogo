// Spike 4 evidence: install and update are one decision table.
//
// The property under test is the one the whole model rests on: after a user edits
// a recipe, an update must be DECIDABLE without a merge engine, and it must never
// be destructive. Each test below pins one row of the decision table.
package recipe

import (
	"strings"
	"testing"
)

const (
	v1 = "package timer\n\nfunc Timer() {}\n"
	v2 = "package timer\n\nfunc Timer() { /* upstream fix */ }\n"
)

// tree answers current() from a map, so the planner is exercised without touching
// the filesystem.
func tree(files map[string]string) func(string) (string, bool) {
	return func(p string) (string, bool) {
		v, ok := files[p]
		return v, ok
	}
}

func opsByPath(p PlanResult) map[string]Op {
	m := map[string]Op{}
	for _, op := range p.Ops {
		m[op.Path] = op
	}
	return m
}

// TestPlanFreshInstallIsAllWrites is the install case: an empty tree and a zero
// receipt mean every upstream file is new. This is what makes install a special
// case of update rather than a separate mechanism.
func TestPlanFreshInstallIsAllWrites(t *testing.T) {
	up := map[string]string{"features/timer/page.go": v1, "features/timer/domain.go": v1}
	p := Plan(Receipt{Files: map[string]string{}}, up, tree(nil))
	if p.Blocked() {
		t.Fatal("a fresh install cannot conflict with itself")
	}
	write, _, _, _ := p.Counts()
	if write != 2 {
		t.Fatalf("expected 2 writes for a fresh install, got %d", write)
	}
}

// TestPlanUntouchedFileFastForwards is the cheap common case: the user changed
// nothing, upstream moved, so the file is replaced with no human involvement.
func TestPlanUntouchedFileFastForwards(t *testing.T) {
	const path = "features/timer/domain.go"
	r := Receipt{Files: map[string]string{path: Hash([]byte(v1))}}
	p := Plan(r, map[string]string{path: v2}, tree(map[string]string{path: v1}))

	op := opsByPath(p)[path]
	if op.Kind != OpFastForward {
		t.Fatalf("expected a fast-forward, got %s", op.Kind)
	}
	if op.Content != v2 {
		t.Fatal("the fast-forward must carry the new upstream content")
	}
}

// TestPlanModifiedAndUpstreamMovedIsAConflict is the case that must never be
// guessed: the user owns those bytes AND upstream changed them. The planner names
// the file and refuses to apply.
func TestPlanModifiedAndUpstreamMovedIsAConflict(t *testing.T) {
	const path = "features/timer/domain.go"
	mine := "package timer\n\nfunc Timer() { /* my thing */ }\n"
	r := Receipt{Files: map[string]string{path: Hash([]byte(v1))}}
	p := Plan(r, map[string]string{path: v2}, tree(map[string]string{path: mine}))

	op := opsByPath(p)[path]
	if op.Kind != OpConflict {
		t.Fatalf("expected a conflict, got %s", op.Kind)
	}
	if !p.Blocked() {
		t.Fatal("a conflict must block the plan: silently overwriting user code is the failure this prevents")
	}
}

// TestPlanModifiedButUpstreamUnchangedKeepsUserBytes is the most important
// non-destructive property: a file the user edited and upstream did not touch is
// left completely alone. If this ever regresses, the model becomes destructive.
func TestPlanModifiedButUpstreamUnchangedKeepsUserBytes(t *testing.T) {
	const path = "features/timer/domain.go"
	mine := "package timer\n\nfunc Timer() { /* my thing */ }\n"
	r := Receipt{Files: map[string]string{path: Hash([]byte(v1))}}
	p := Plan(r, map[string]string{path: v1}, tree(map[string]string{path: mine}))

	op := opsByPath(p)[path]
	if op.Kind != OpKeep {
		t.Fatalf("expected the user's file to be kept, got %s", op.Kind)
	}
	if op.Content != "" {
		t.Fatal("a kept file must carry no content: writing it would overwrite the user")
	}
}

// TestPlanUpstreamDropOfUntouchedFileIsObsolete asserts cleanup is possible
// without risking user work: upstream removed a file the user never touched.
func TestPlanUpstreamDropOfUntouchedFileIsObsolete(t *testing.T) {
	const path = "features/timer/old.go"
	r := Receipt{Files: map[string]string{path: Hash([]byte(v1))}}
	p := Plan(r, map[string]string{}, tree(map[string]string{path: v1}))

	if opsByPath(p)[path].Kind != OpObsolete {
		t.Fatalf("expected obsolete, got %s", opsByPath(p)[path].Kind)
	}
}

// TestPlanUpstreamDropOfModifiedFileKeepsIt is the same rule applied to a file the
// user edited: upstream dropping it must not delete their work.
func TestPlanUpstreamDropOfModifiedFileKeepsIt(t *testing.T) {
	const path = "features/timer/old.go"
	mine := "package timer\n\nfunc Old() { /* mine */ }\n"
	r := Receipt{Files: map[string]string{path: Hash([]byte(v1))}}
	p := Plan(r, map[string]string{}, tree(map[string]string{path: mine}))

	if opsByPath(p)[path].Kind != OpConflict {
		t.Fatalf("a modified file upstream dropped must surface, got %s", opsByPath(p)[path].Kind)
	}
	if !p.Blocked() {
		t.Fatal("it must block rather than delete the user's work")
	}
}

// TestPlanUserDeletedFileIsNotResurrected asserts that a deliberate removal is
// respected: the planner reports it as gone rather than writing the file back.
func TestPlanUserDeletedFileIsNotResurrected(t *testing.T) {
	const path = "features/timer/domain.go"
	r := Receipt{Files: map[string]string{path: Hash([]byte(v1))}}
	p := Plan(r, map[string]string{path: v2}, tree(nil))

	op := opsByPath(p)[path]
	if op.Kind != OpGone {
		t.Fatalf("expected gone, got %s", op.Kind)
	}
	if op.Content != "" {
		t.Fatal("a deleted file must not be rewritten: removal was user intent")
	}
}

// TestPlanIsDeterministic asserts operations are in path order, so a plan read by a
// human or diffed in a review is stable.
func TestPlanIsDeterministic(t *testing.T) {
	up := map[string]string{"z.go": v1, "a.go": v1, "m.go": v1}
	p := Plan(Receipt{Files: map[string]string{}}, up, tree(nil))
	got := make([]string, 0, len(p.Ops))
	for _, op := range p.Ops {
		got = append(got, op.Path)
	}
	if strings.Join(got, ",") != "a.go,m.go,z.go" {
		t.Fatalf("expected path order, got %v", got)
	}
}

// TestPlanRenderNamesOwnership asserts the report never describes the user's edit
// as a problem. Language matters here: a report that calls a modified file an
// issue trains people to overwrite it.
func TestPlanRenderNamesOwnership(t *testing.T) {
	const path = "features/timer/domain.go"
	mine := "package timer\n\nfunc Timer() { /* mine */ }\n"
	r := Receipt{Files: map[string]string{path: Hash([]byte(v1))}}
	p := Plan(r, map[string]string{path: v1}, tree(map[string]string{path: mine}))

	var b strings.Builder
	p.Render(&b)
	out := b.String()
	if !strings.Contains(out, "(yours)") {
		t.Fatalf("a kept user file must be labelled as theirs, got:\n%s", out)
	}
}
