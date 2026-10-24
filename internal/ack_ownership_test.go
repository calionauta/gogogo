// SCOPE:core — the ownership rule for enqueueing endpoints.
//
// An endpoint that enqueues a job answers on its OWN response, while the
// result arrives on the tab's already-open SSE stream: two connections, no
// ordering between them, and the response is the slower one whenever the
// caller is behind a tunnel (or Datastar retries the POST). So an ack that
// writes a state signal can land AFTER the worker's terminal patch and undo it.
//
// The four endpoints that got this wrong, and what a late ack cost each:
//
//	/api/todos/suggest    aiPending, suggestions  → spinner stuck, results erased
//	/api/todos/retry-demo  lastRetry               → the attempt text overwritten
//	/api/chat/ask         chat_thinking           → Send button dead forever
//
// That last one is the worst: its button is `data-attr:disabled="!!$x"`, so a
// re-asserted `true` is a DEAD control, not a stuck spinner.
//
// The rule, in one line: an ack that enqueues a job writes NO state signal.
// The client owns the optimistic state, the worker owns the terminal state.
//
// This test is the mechanical half. The judgement half (which signal is
// "state" and which is the client's own) is per endpoint — but the shape
// "an enqueueing endpoint's ack must not carry a pending/thinking state
// signal" is checkable, and it is what catches the NEXT endpoint that copies
// the old pattern.
package internal_test

import (
	"go/ast"
	"go/parser"
	"go/token"
	"os"
	"path/filepath"
	"strconv"
	"strings"
	"testing"
)

// stateSignalNames are the signal names that belong to a worker/terminal
// state. An ack must not write them.
var stateSignalNames = map[string]bool{
	"aiPending":        true,
	"aiPhase":          true,
	"suggestions":      true,
	"suggestErr":       true,
	"lastRetry":        true,
	"lastRetryStatus":  true,
	"lastRetryAttempt": true,
	"chat_thinking":    true,
	"chat_error":       true,
	"genui_pending":    true,
	"genui_error":      true,
}

// enqueueingHandler is a handler that enqueues a job: the function body
// contains a call to something named Enqueue.
func isEnqueueingHandler(fn *ast.FuncDecl) bool {
	if fn.Body == nil {
		return false
	}
	found := false
	ast.Inspect(fn.Body, func(n ast.Node) bool {
		call, ok := n.(*ast.CallExpr)
		if !ok {
			return true
		}
		sel, ok := call.Fun.(*ast.SelectorExpr)
		if !ok {
			return true
		}
		if sel.Sel.Name == "Enqueue" {
			found = true
		}
		return true
	})
	return found
}

// signalKeysIn returns the signal names a composite literal writes, as string
// keys or as identifiers of constants whose VALUE is a string.
func signalKeysIn(file *ast.File, lit *ast.CompositeLit) []string {
	var out []string
	for _, elt := range lit.Elts {
		kv, ok := elt.(*ast.KeyValueExpr)
		if !ok {
			continue
		}
		switch key := kv.Key.(type) {
		case *ast.BasicLit:
			if key.Kind == token.STRING {
				if s, err := strconv.Unquote(key.Value); err == nil {
					out = append(out, s)
				}
			}
		case *ast.Ident:
			// A named constant: resolve it to its string value in the file.
			if v, ok := stringConstValue(file, key.Name); ok {
				out = append(out, v)
			}
		}
	}
	return out
}

// stringConstValue resolves a local string constant to its value.
func stringConstValue(file *ast.File, name string) (string, bool) {
	for _, decl := range file.Decls {
		gd, ok := decl.(*ast.GenDecl)
		if !ok || gd.Tok != token.CONST {
			continue
		}
		for _, spec := range gd.Specs {
			vs, ok := spec.(*ast.ValueSpec)
			if !ok {
				continue
			}
			for i, n := range vs.Names {
				if n.Name != name || i >= len(vs.Values) {
					continue
				}
				bl, ok := vs.Values[i].(*ast.BasicLit)
				if ok && bl.Kind == token.STRING {
					if s, err := strconv.Unquote(bl.Value); err == nil {
						return s, true
					}
				}
			}
		}
	}
	return "", false
}

// TestEnqueueingAcksWriteNoStateSignal walks the feature handlers and fails
// when an enqueueing handler merges a state signal into its ack.
//
// The check is deliberately about the SHAPE (an ack that writes a state
// signal), not about a specific endpoint: that is what keeps the next
// endpoint that copies the pattern from shipping.
func TestEnqueueingAcksWriteNoStateSignal(t *testing.T) {
	t.Parallel()
	root := repoRoot(t)

	var checked int
	// ackShapes counts the MergeSignals calls the rule actually inspected.
	// Without it, a refactor that moves the ack behind a helper (say
	// dshelpers.AckAccepted) would leave the walk looking at handlers it no
	// longer recognises, PASS vacuously, and tell nobody the rule went blind.
	var ackShapes int
	err := filepath.WalkDir(root, func(path string, d os.DirEntry, err error) error {
		if err != nil {
			return err
		}
		if d.IsDir() {
			base := filepath.Base(path)
			switch base {
			case ".git", "node_modules", "site", "vendor":
				return filepath.SkipDir
			}
			return nil
		}
		if !strings.HasSuffix(path, ".go") || strings.HasSuffix(path, "_test.go") {
			return nil
		}
		rel, rerr := filepath.Rel(root, path)
		if rerr != nil {
			return rerr
		}
		if !strings.HasPrefix(rel, "features"+string(filepath.Separator)) &&
			!strings.HasPrefix(rel, "internal"+string(filepath.Separator)) {
			return nil
		}

		fset := token.NewFileSet()
		file, perr := parser.ParseFile(fset, path, nil, 0)
		if perr != nil {
			// A file that does not parse is a compile error elsewhere; not this
			// test's business.
			//nolint:nilerr // deliberately skipping: the compiler owns parse errors
			return nil
		}
		for _, decl := range file.Decls {
			fn, ok := decl.(*ast.FuncDecl)
			if !ok || !isEnqueueingHandler(fn) {
				continue
			}
			checked++
			ast.Inspect(fn.Body, func(n ast.Node) bool {
				call, ok := n.(*ast.CallExpr)
				if !ok {
					return true
				}
				// MergeSignals(sse, map[string]any{...}) — the ack shape.
				sel, ok := call.Fun.(*ast.SelectorExpr)
				if !ok || sel.Sel.Name != "MergeSignals" || len(call.Args) < 2 {
					return true
				}
				lit, ok := call.Args[1].(*ast.CompositeLit)
				if !ok {
					return true
				}
				ackShapes++
				// A patch inside an ENQUEUE-FAILURE branch is the opposite of a
				// bug: there is no worker to deliver a terminal patch (the job
				// never ran), so the failure path MUST write the terminal state
				// itself, or the optimistic spinner/disabled button has nothing
				// to clear it. Only the SUCCESS path is constrained.
				if inEnqueueFailureBranch(fn.Body, call) {
					return true
				}
				for _, key := range signalKeysIn(file, lit) {
					if stateSignalNames[key] {
						t.Errorf("%s:%d: %s enqueues a job and its SUCCESS ack writes the state signal %q — "+
							"a late ack can land after the worker's terminal patch and undo it "+
							"(the composer owns the optimistic state, the worker the terminal one)",
							rel, fset.Position(call.Pos()).Line, fn.Name.Name, key)
					}
				}
				return true
			})
		}
		return nil
	})
	if err != nil {
		t.Fatalf("walk repo: %v", err)
	}
	if checked == 0 {
		t.Fatal("no enqueueing handler found — the rule would be vacuous")
	}
	if ackShapes == 0 {
		t.Fatal("no ack (MergeSignals with a signal map) was inspected inside an enqueueing handler — " +
			"the walk no longer recognises the ack shape, so a new endpoint that copies the old pattern would pass")
	}
}

// repoRoot walks up from the test's working directory to the module root.
func repoRoot(t *testing.T) string {
	t.Helper()
	dir, err := os.Getwd()
	if err != nil {
		t.Fatalf("getwd: %v", err)
	}
	for {
		if _, serr := os.Stat(filepath.Join(dir, "go.mod")); serr == nil {
			return dir
		}
		parent := filepath.Dir(dir)
		if parent == dir {
			t.Fatal("go.mod not found above the test directory")
		}
		dir = parent
	}
}

// inEnqueueFailureBranch reports whether the call sits inside a branch that
// handles an Enqueue error — the `if err := ...Enqueue(...); err != nil { ... }`
// block, or a nested one.
//
// The check is structural: walk the function body for the Enqueue call, then
// see whether the MergeSignals call is inside that call's error branch.
func inEnqueueFailureBranch(body *ast.BlockStmt, target *ast.CallExpr) bool {
	found := false
	ast.Inspect(body, func(n ast.Node) bool {
		if found {
			return false
		}
		ifn, ok := n.(*ast.IfStmt)
		if !ok || ifn.Init == nil {
			return true
		}
		assign, ok := ifn.Init.(*ast.AssignStmt)
		if !ok {
			return true
		}
		for _, rhs := range assign.Rhs {
			call, ok := rhs.(*ast.CallExpr)
			if !ok {
				continue
			}
			sel, ok := call.Fun.(*ast.SelectorExpr)
			if !ok || sel.Sel.Name != "Enqueue" {
				continue
			}
			// Inside this error branch?
			ast.Inspect(ifn.Body, func(m ast.Node) bool {
				if m == target {
					found = true
					return false
				}
				return !found
			})
		}
		return !found
	})
	return found
}
