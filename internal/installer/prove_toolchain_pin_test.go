// SCOPE:plugin — installer engine: the toolchain pin proof
package installer

import (
	"bytes"
	"context"
	"errors"
	"os"
	"path/filepath"
	"strings"
	"testing"
)

// TestProveToolchainPinSkipsWithoutTheSurface: a synthetic fixture has no
// internal/ directory, and the proof must not invent a failure there.
func TestProveToolchainPinSkipsWithoutTheSurface(t *testing.T) {
	t.Parallel()
	dir := t.TempDir()
	var out bytes.Buffer

	if err := proveToolchainPin(context.Background(), dir, &out); err != nil {
		t.Fatalf("proveToolchainPin on a tree without the pin surface: %v", err)
	}
	if out.Len() != 0 {
		t.Errorf("the proof announced work it did not do: %q", out.String())
	}
}

// TestProveToolchainPinRunsWhenTheSurfaceIsThere is the guard against a proof
// that never runs: the file exists, the test fails, and prove must exit 2 with
// the compiler output rather than reporting success.
func TestProveToolchainPinRunsWhenTheSurfaceIsThere(t *testing.T) {
	t.Parallel()
	dir := t.TempDir()
	writeFile(t, filepath.Join(dir, "go.mod"), "module example.com/pinproof\n\ngo 1.27\n")
	writeFile(t, filepath.Join(dir, "internal", "toolchain_pin_test.go"),
		"package internal_test\n\nimport \"testing\"\n\n"+
			"func TestGoToolchainPinIsConsistent(t *testing.T) {\n"+
			"\tt.Fatal(\"the surface names a path that does not exist\")\n"+
			"}\n")

	var out bytes.Buffer
	err := proveToolchainPin(context.Background(), dir, &out)
	if err == nil {
		t.Fatal("proveToolchainPin passed although the pin test failed")
	}
	var exitErr *ExitError
	if !errors.As(err, &exitErr) || exitErr.code != 2 {
		t.Fatalf("want an ExitError with code 2, got %#v", err)
	}

	// And the passing half: a green test must not fail the proof.
	writeFile(t, filepath.Join(dir, "internal", "toolchain_pin_test.go"),
		"package internal_test\n\nimport \"testing\"\n\n"+
			"func TestGoToolchainPinIsConsistent(t *testing.T) {}\n")
	out.Reset()
	if err := proveToolchainPin(context.Background(), dir, &out); err != nil {
		t.Fatalf("proveToolchainPin rejected a green pin test: %v", err)
	}
	if _, statErr := os.Stat(filepath.Join(dir, "internal", "toolchain_pin_test.go")); statErr != nil {
		t.Fatalf("fixture vanished: %v", statErr)
	}
}

// TestProveFailsWhenThePinSurfaceBreaks is the wiring guard: a proof nobody
// calls is not a proof. Same tree the scaffold e2e uses, plus the pin surface
// pointing at a file that does not exist.
func TestProveFailsWhenThePinSurfaceBreaks(t *testing.T) {
	t.Parallel()
	dir := t.TempDir()
	writeScaffoldFixture(t, dir, fixtureMainOK)
	writeFile(t, filepath.Join(dir, "internal", "toolchain_pin_test.go"),
		"package internal_test\n\nimport \"testing\"\n\n"+
			"func TestGoToolchainPinIsConsistent(t *testing.T) {\n"+
			"\tt.Fatal(\"cmd/gogogo-mcp/go.mod: no such file\")\n"+
			"}\n")

	var out bytes.Buffer
	err := prove(context.Background(), dir, nil, &out)
	if err == nil {
		t.Fatalf("prove passed although the pin surface is broken (output: %s)", out.String())
	}
	var exitErr *ExitError
	if !errors.As(err, &exitErr) || exitErr.code != 2 {
		t.Fatalf("want an ExitError with code 2, got %#v", err)
	}
	if !strings.Contains(out.String(), "TestGoToolchainPin") {
		t.Errorf("the proof never announced the pin check:\n%s", out.String())
	}
}
