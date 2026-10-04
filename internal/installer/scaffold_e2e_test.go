package installer

import (
	"context"
	"encoding/json"
	"os"
	"path/filepath"
	"strings"
	"testing"
)

// writeScaffoldFixture builds the smallest tree the apply path accepts:
// a go.mod carrying the template module path plus a compilable cmd/web.
// drop=[] keeps everything, so the proof compiles this exact tree.
func writeScaffoldFixture(t *testing.T, dir, mainGo string) {
	t.Helper()
	writeFile(t, filepath.Join(dir, "go.mod"),
		"module github.com/calionauta/gogogo\n\ngo 1.27\n")
	writeFile(t, filepath.Join(dir, "cmd", "web", "main.go"), mainGo)
}

const fixtureMainOK = "package main\n\nfunc main() {}\n"

func TestScaffoldKeepAllRealProof(t *testing.T) {
	// Full Run: plan, preflight (real go+git), apply, rename, AGENTS.md,
	// and a REAL `go mod tidy` + `go build ./cmd/web`. Slow on purpose:
	// this is the one test that breaks when the happy path breaks.
	dir := t.TempDir()
	writeScaffoldFixture(t, dir, fixtureMainOK)
	out := captureStdout(t, func(w *os.File) {
		err := Run(context.Background(), []string{
			"--name", "e2e-app", "--owner", "e2eorg",
			"--no-tui", "--yes", "--dir", dir,
		}, devNull(t), w)
		if err != nil {
			t.Fatalf("run: %v", err)
		}
	})
	if !strings.Contains(out, "build OK") {
		t.Errorf("proof did not report build OK:\n%s", out)
	}
	agents, err := os.ReadFile(filepath.Join(dir, "AGENTS.md"))
	if err != nil {
		t.Fatalf("AGENTS.md missing: %v", err)
	}
	if !strings.Contains(string(agents), "e2e-app") {
		t.Error("AGENTS.md does not name the scaffolded project")
	}
	gomod, err := os.ReadFile(filepath.Join(dir, "go.mod"))
	if err != nil {
		t.Fatalf("go.mod missing: %v", err)
	}
	if !strings.Contains(string(gomod), "module github.com/e2eorg/e2e-app") {
		t.Errorf("go.mod was not renamed:\n%s", gomod)
	}
}

func TestScaffoldBrokenProofReports(t *testing.T) {
	// Red-proof for the gate above: a tree that cannot compile must fail
	// the apply with the compiler output AND a buildOk:false envelope —
	// never a silent success.
	dir := t.TempDir()
	writeScaffoldFixture(t, dir, "package main\n\nfunc broken( {\n")
	out := captureStdout(t, func(w *os.File) {
		err := Run(context.Background(), []string{
			"--name", "e2e-broken", "--owner", "e2eorg",
			"--no-tui", "--yes", "--format", "json", "--dir", dir,
		}, devNull(t), w)
		if err == nil {
			t.Fatal("expected the proof build to fail")
		}
	})
	var env struct {
		BuildOk bool `json:"buildOk"`
		Next    struct {
			Dir string `json:"dir"`
		} `json:"next"`
	}
	if err := json.Unmarshal([]byte(out), &env); err != nil {
		t.Fatalf("broken apply is not valid JSON: %v\n%s", err, out)
	}
	if env.BuildOk {
		t.Error("buildOk must be false for an uncompilable tree")
	}
	if env.Next.Dir != dir {
		t.Errorf("envelope.next.dir = %q, want %q", env.Next.Dir, dir)
	}
}

func TestPreflightRealTools(t *testing.T) {
	// The suite itself needs go to compile, and the tree needs git to be
	// a checkout: assert the happy path with the real binaries, not stubs.
	var out strings.Builder
	if err := preflight(context.Background(), &out, true, true, false); err != nil {
		t.Fatalf("real preflight should pass here: %v", err)
	}
	if !strings.Contains(out.String(), "toolchain OK") {
		t.Errorf("pass should narrate the toolchain, got %q", out.String())
	}
}
