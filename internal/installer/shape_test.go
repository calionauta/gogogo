package installer

import (
	"bytes"
	"os"
	"path/filepath"
	"strings"
	"testing"
)

// TestShapeCheckReportsAConsistentTree pins the subcommand's happy path: the
// same rules as cmd/check-shape, printed on stdout.
func TestShapeCheckReportsAConsistentTree(t *testing.T) {
	root := t.TempDir()
	if err := os.MkdirAll(filepath.Join(root, "cmd", "web"), 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(root, "cmd", "web", "main.go"),
		[]byte("package main\n\nfunc main() {}\n"), 0o600); err != nil {
		t.Fatal(err)
	}
	if err := os.MkdirAll(filepath.Join(root, ".gogogo"), 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(root, ".gogogo", "shape.yml"),
		[]byte("shape: web\n"), 0o600); err != nil {
		t.Fatal(err)
	}
	var out bytes.Buffer
	if err := runShape([]string{"check", "--dir", root}, &out); err != nil {
		t.Fatalf("runShape() = %v", err)
	}
	if !strings.Contains(out.String(), "shape check: web") {
		t.Fatalf("runShape() printed %q, want the shape verdict", out.String())
	}
}

// TestShapeCheckFailsOnABrokenRecord proves the subcommand returns the gate's
// verdict as a non-zero exit rather than a silent success.
func TestShapeCheckFailsOnABrokenRecord(t *testing.T) {
	var out bytes.Buffer
	if err := runShape([]string{"check", "--dir", t.TempDir()}, &out); err == nil {
		t.Fatal("runShape() = nil, want the missing-entrypoint failure")
	}
}

// TestShapeNeedsASubcommand keeps the surface honest: the record is written by
// the scaffolder, so there is nothing else to do here.
func TestShapeNeedsASubcommand(t *testing.T) {
	var out bytes.Buffer
	if err := runShape(nil, &out); err == nil {
		t.Fatal("runShape(nil) = nil error, want a usage error")
	}
}
