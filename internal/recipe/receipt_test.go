// Spike 2 evidence: the four file states and the declaration checks.
//
// These tests are the spike's artifact. Each one asserts a property the recipe
// model needs to be true, and several assert that a FAILURE is detected, which is
// the property that actually matters: a drift check that cannot fail is not a
// check.
package recipe

import (
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"
)

// writeTree lays down a small installed recipe and returns its root.
func writeTree(t *testing.T, files map[string]string) string {
	t.Helper()
	root := t.TempDir()
	for p, body := range files {
		full := filepath.Join(root, filepath.FromSlash(p))
		if err := os.MkdirAll(filepath.Dir(full), 0o755); err != nil {
			t.Fatal(err)
		}
		if err := os.WriteFile(full, []byte(body), filePerm); err != nil {
			t.Fatal(err)
		}
	}
	return root
}

// receiptFor builds a receipt whose hashes match the given pristine content.
func receiptFor(files map[string]string, decl Declaration) Receipt {
	r := Receipt{ID: "timer", Files: map[string]string{}, Declaration: decl}
	r.Upstream.Repo = "github.com/calionauta/gogogo"
	r.Upstream.Ref = "v0.56.0"
	r.Upstream.Path = "features/timer"
	for p, body := range files {
		r.Files[p] = Hash([]byte(body))
	}
	return r
}

const pageSrc = `package timer

func Routes() {
	handle("GET", "/timer", page)
	registerJob("ext:timer:tick")
}
`

// TestDriftFourFileStates is the spike's core assertion: an edited tree must be
// reported as four distinguishable states, because each state has a DIFFERENT
// safe action. Collapsing them is what makes an update either destructive or
// useless.
func TestDriftFourFileStates(t *testing.T) {
	files := map[string]string{
		"features/timer/page.go":   pageSrc,
		"features/timer/domain.go": "package timer\n\nfunc Timer() {}\n",
		"features/timer/dead.go":   "package timer\n\nfunc Dead() {}\n",
	}
	root := writeTree(t, files)
	r := receiptFor(files, Declaration{
		Routes: []Route{{Method: "GET", Pattern: "/timer"}},
		Jobs:   []string{"ext:timer:tick"},
	})

	// The user edits one file and deletes another.
	edited := "package timer\n\nfunc Timer() { /* mine now */ }\n"
	if err := os.WriteFile(filepath.Join(root, "features/timer/domain.go"), []byte(edited), filePerm); err != nil {
		t.Fatal(err)
	}
	if err := os.Remove(filepath.Join(root, "features/timer/dead.go")); err != nil {
		t.Fatal(err)
	}

	d, err := Check(root, r)
	if err != nil {
		t.Fatal(err)
	}
	got := map[string]FileState{}
	for _, f := range d.Files {
		got[f.Path] = f.State
	}
	want := map[string]FileState{
		"features/timer/page.go":   StatePristine,
		"features/timer/domain.go": StateModified,
		"features/timer/dead.go":   StateDeleted,
	}
	for p, w := range want {
		if got[p] != w {
			t.Errorf("%s: want %s, got %s", p, w, got[p])
		}
	}
	if d.Pristine() {
		t.Error("a tree with an edited file must not report pristine: an update would be unsafe")
	}
}

// TestDriftUntouchedTreeIsFastForwardable asserts the property that makes the
// model inexpensive in the common case: a user who changed nothing gets a
// straight fast-forward, no merge, no prompts.
func TestDriftUntouchedTreeIsFastForwardable(t *testing.T) {
	files := map[string]string{"features/timer/page.go": pageSrc}
	root := writeTree(t, files)
	d, err := Check(root, receiptFor(files, Declaration{}))
	if err != nil {
		t.Fatal(err)
	}
	if !d.Pristine() {
		t.Fatalf("expected pristine, got %+v", d.Files)
	}
}

// TestDriftCatchesDeclaredRouteWithNoCode asserts the check that a version
// string cannot do: the declaration claims a route the code never registers.
// This is the drift that ships a broken nav entry.
func TestDriftCatchesDeclaredRouteWithNoCode(t *testing.T) {
	files := map[string]string{"features/timer/page.go": pageSrc}
	root := writeTree(t, files)
	d, err := Check(root, receiptFor(files, Declaration{
		Routes: []Route{{Method: "GET", Pattern: "/timer/settings"}},
	}))
	if err != nil {
		t.Fatal(err)
	}
	if len(d.Problems) == 0 {
		t.Fatal("a declared route absent from the code must be reported")
	}
	if !strings.Contains(d.Problems[0], "/timer/settings") {
		t.Errorf("problem must name the route: %v", d.Problems)
	}
}

// TestDriftCatchesDeclaredJobWithNoRegistration is the same property for a
// background job, which is the case where a silent drift burns CPU forever
// instead of breaking a page.
func TestDriftCatchesDeclaredJobWithNoRegistration(t *testing.T) {
	files := map[string]string{"features/timer/page.go": pageSrc}
	root := writeTree(t, files)
	d, err := Check(root, receiptFor(files, Declaration{
		Jobs: []string{"ext:timer:remind"},
	}))
	if err != nil {
		t.Fatal(err)
	}
	if len(d.Problems) == 0 {
		t.Fatal("a declared job the code never registers must be reported")
	}
}

// TestDriftRefusesRecipeOwningACoreTable asserts the hard boundary: a recipe may
// declare its own tables, never a core one. This is the rule that keeps
// uninstall safe and keeps two owners out of one schema.
func TestDriftRefusesRecipeOwningACoreTable(t *testing.T) {
	// The recipe's own SQL mentions a core table, so the SQL-presence check alone
	// would be satisfied. The boundary must still fire — that is the difference
	// between a rule and a coincidence.
	files := map[string]string{
		"features/timer/page.go":    pageSrc,
		"features/timer/schema.sql": "SELECT * FROM todos;\nINSERT INTO users VALUES (1);\n",
	}
	root := writeTree(t, files)
	for _, core := range []string{"todos", "users", "sessions"} {
		d, err := Check(root, receiptFor(files, Declaration{Tables: []string{core}}))
		if err != nil {
			t.Fatal(err)
		}
		if len(d.Problems) == 0 {
			t.Errorf("declaring the core table %q must be refused", core)
			continue
		}
		if !strings.Contains(d.Problems[0], "core table") {
			t.Errorf("the refusal must name the ownership boundary, got %q", d.Problems[0])
		}
	}
}

// TestDriftAcceptsAMatchingDeclaration is the negative control: without it,
// every check above could be passing by rejecting everything.
func TestDriftAcceptsAMatchingDeclaration(t *testing.T) {
	files := map[string]string{
		"features/timer/page.go":    pageSrc,
		"features/timer/schema.sql": "CREATE TABLE ext_timer_session (id TEXT);\n",
	}
	root := writeTree(t, files)
	d, err := Check(root, receiptFor(files, Declaration{
		Routes: []Route{{Method: "GET", Pattern: "/timer"}},
		Jobs:   []string{"ext:timer:tick"},
		Tables: []string{"ext_timer_session"},
	}))
	if err != nil {
		t.Fatal(err)
	}
	if len(d.Problems) != 0 {
		t.Fatalf("a matching declaration must produce no problems, got %v", d.Problems)
	}
}

// TestDriftAcceptsADeclaredSlotTheCodeContributes is the slot half's negative
// control. The contribution is spelled the way real code spells it — the Go
// constant, not the id string — so the check cannot pass by demanding a spelling
// no recipe uses.
func TestDriftAcceptsADeclaredSlotTheCodeContributes(t *testing.T) {
	files := map[string]string{
		"features/timer/register.go": "package timer\n\nvar slot = recipe.SlotSettingsSection\n",
	}
	root := writeTree(t, files)
	d, err := Check(root, receiptFor(files, Declaration{Slots: []string{"settings.section"}}))
	if err != nil {
		t.Fatal(err)
	}
	if len(d.Problems) != 0 {
		t.Fatalf("a slot the code contributes must produce no problems, got %v", d.Problems)
	}
}

// TestDriftReportsADeclaredSlotNothingContributes is the direction no file hash
// can answer: the tree can be byte-pristine and the declaration still claim
// content that no code contributes.
func TestDriftReportsADeclaredSlotNothingContributes(t *testing.T) {
	files := map[string]string{"features/timer/register.go": "package timer\n"}
	root := writeTree(t, files)
	d, err := Check(root, receiptFor(files, Declaration{Slots: []string{"settings.section"}}))
	if err != nil {
		t.Fatal(err)
	}
	if len(d.Problems) != 1 || !strings.Contains(d.Problems[0], "not contributed") {
		t.Fatalf("a declared slot with no contribution must be reported, got %v", d.Problems)
	}
}

// TestDriftAcceptsAHostedDashboardCard flips the old refusal reporting: the
// dashboard page exists now, so a receipt declaring it drifts against nothing
// and Check has no problem to report. The unhosted direction is closed by
// registration's refuseSlot, which the shape-only case still exercises via
// an unknown slot name below.
func TestDriftAcceptsAHostedDashboardCard(t *testing.T) {
	files := map[string]string{
		"features/timer/register.go": "package timer\n\nvar slot = recipe.SlotDashboardCard\n",
	}
	root := writeTree(t, files)
	d, err := Check(root, receiptFor(files, Declaration{Slots: []string{"dashboard.card"}}))
	if err != nil {
		t.Fatal(err)
	}
	if len(d.Problems) != 0 {
		t.Fatalf("a hosted slot must drift against nothing, got %v", d.Problems)
	}
}

// TestReceiptHashIsContentAddressed records why the receipt stores hashes rather
// than a version: two different trees can share a version, but not a hash.
func TestReceiptHashIsContentAddressed(t *testing.T) {
	a := Hash([]byte("hello"))
	b := Hash([]byte("hello!"))
	if a == b {
		t.Fatal("hashes must differ for different content")
	}
	if a != Hash([]byte("hello")) {
		t.Fatal("hashing must be deterministic")
	}
	if len(a) != 64 {
		t.Fatalf("expected a hex sha256, got %d chars", len(a))
	}
}

// TestReceiptStateRefusesDoubleRemove is the FSM's one refusal: a second
// uninstall would overwrite the timestamp that says when the files left, so
// the transition is rejected rather than made idempotent by accident.
func TestReceiptStateRefusesDoubleRemove(t *testing.T) {
	r := Receipt{ID: "todo"}
	if r.State() != StateInstalled {
		t.Fatalf("fresh receipt state = %s, want installed", r.State())
	}
	at := time.Date(2026, 11, 13, 10, 0, 0, 0, time.UTC)
	if err := r.MarkRemoved(at); err != nil {
		t.Fatalf("MarkRemoved: %v", err)
	}
	if r.State() != StateRemoved {
		t.Fatalf("state after remove = %s, want removed", r.State())
	}
	if r.Removed != at.Format(time.RFC3339) {
		t.Fatalf("Removed = %q, want %s", r.Removed, at.Format(time.RFC3339))
	}
	err := r.MarkRemoved(at.Add(time.Hour))
	if err == nil {
		t.Fatal("second MarkRemoved succeeded; the timestamp is now a lie")
	}
	if !strings.Contains(err.Error(), "already removed") {
		t.Errorf("error = %q, want it to say already removed", err)
	}
	if r.Removed != at.Format(time.RFC3339) {
		t.Errorf("refused transition rewrote the timestamp: %q", r.Removed)
	}

	r.MarkInstalled()
	if r.State() != StateInstalled {
		t.Fatalf("state after reinstall = %s, want installed", r.State())
	}
	if err := r.MarkRemoved(at.Add(2 * time.Hour)); err != nil {
		t.Fatalf("remove after reinstall: %v", err)
	}
}
