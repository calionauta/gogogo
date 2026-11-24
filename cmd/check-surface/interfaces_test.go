package main

import (
	"go/token"
	"go/types"
	"testing"
)

// The interface collector decides which interfaces may keep a method alive.
// Every rule in it exists because ignoring it produced a wrong answer, so each
// gets a test that FAILS when the rule is removed — a rule without one is a
// comment, and this file is the difference between the two.

// TestInterfaceSinkIgnoresEmpty is the guard that stops `any` from crediting
// everything. An empty interface is satisfied by every type, so collecting one
// would mark the whole module reachable and the report would always be empty.
//
// Reachability note: add() is only ever called with a package's TypeNames, and
// `any`/`interface{}` is not declared as one, so the guard cannot be reached
// through moduleInterfaces. That is exactly why this test calls the sink
// directly: an unreachable guard with no test is dead code that reads as
// protection.
func TestInterfaceSinkIgnoresEmpty(t *testing.T) {
	var got []*types.Interface
	seen := map[string]bool{}
	add := interfaceSink(&got, seen)

	add(types.NewInterfaceType(nil, nil)) // the empty interface, i.e. `any`

	if len(got) != 0 {
		t.Fatalf("collected %d interface(s) including the empty one; `any` would credit every type", len(got))
	}
}

// TestInterfaceSinkKeepsANonEmpty pins the other half: the guard must not be so
// broad that it drops real contracts.
func TestInterfaceSinkKeepsANonEmpty(t *testing.T) {
	var got []*types.Interface
	seen := map[string]bool{}
	add := interfaceSink(&got, seen)

	pkg := types.NewPackage("example/lib", "lib")
	sig := types.NewSignatureType(nil, nil, nil, nil, nil, false)
	iface := types.NewInterfaceType([]*types.Func{
		types.NewFunc(token.NoPos, pkg, "Talk", sig),
	}, nil)
	iface.Complete()
	add(types.NewNamed(types.NewTypeName(token.NoPos, pkg, "Talker", nil), iface, nil))

	if len(got) != 1 {
		t.Fatalf("collected %d interface(s), want 1 (Talker is a real contract)", len(got))
	}
	if got[0].NumMethods() != 1 {
		t.Fatalf("kept interface has %d method(s), want 1", got[0].NumMethods())
	}
}

// TestInterfaceSinkDeduplicates: the same contract reached by many paths is one
// interface, or satisfaction is re-tested per path for nothing.
func TestInterfaceSinkDeduplicates(t *testing.T) {
	var got []*types.Interface
	seen := map[string]bool{}
	add := interfaceSink(&got, seen)

	pkg := types.NewPackage("example/lib", "lib")
	sig := types.NewSignatureType(nil, nil, nil, nil, nil, false)
	iface := types.NewInterfaceType([]*types.Func{
		types.NewFunc(token.NoPos, pkg, "Close", sig),
	}, nil)
	iface.Complete()
	named := types.NewNamed(types.NewTypeName(token.NoPos, pkg, "Closer", nil), iface, nil)

	add(named)
	add(named)
	add(named.Underlying())

	if len(got) != 1 {
		t.Fatalf("collected %d interface(s) for one contract, want 1", len(got))
	}
}
