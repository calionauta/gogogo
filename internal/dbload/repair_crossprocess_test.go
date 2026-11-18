package dbload

import (
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"testing"
)

// helperEnv makes the test binary act as the child: first Ensure in a fresh
// process, which is the only position the repair can ever run from.
//
// The child runs the REAL loader against the REAL cache layout. Earlier tests
// here mock verifyOrRepair's two closures, which is why a purge that silently
// deleted nothing passed them and failed only on a real boot.
const helperEnv = "DBLOAD_HELPER_ENSURE"

func TestHelperEnsure(t *testing.T) {
	if os.Getenv(helperEnv) != "1" {
		// Not a helper invocation: assert the skip's premise so the test still
		// pins something when the whole package runs.
		if os.Getenv("GOGOGO_FAIL_ON_MISSING_HELPER_ENV") == "1" {
			t.Fatalf("%s was set but %s was not — the parent always sets both", "GOGOGO_FAIL_ON_MISSING_HELPER_ENV", helperEnv)
		}
		t.Skip("helper process only: " + helperEnv + " is not 1")
	}
	if err := Ensure(); err != nil {
		os.Stderr.WriteString("ENSURE_FAILED: " + err.Error() + "\n")
		os.Exit(3)
	}
	// Report what is actually on disk, not just "no error": the repair this
	// pins once deleted nothing and still returned nil, so a bare exit code
	// would have passed.
	libs, _ := filepath.Glob(filepath.Join(cacheDir(), "*", "*"))
	var bytes int64
	for _, lib := range libs {
		if info, statErr := os.Stat(lib); statErr == nil {
			bytes += info.Size()
		}
	}
	fmt.Fprintf(os.Stdout, "ENSURE_OK files=%d bytes=%d\n", len(libs), bytes)
	os.Exit(0)
}

// TestRepairHealsACorruptedCacheAcrossProcesses is the end-to-end proof of the
// repair, in the only shape it can work: a FRESH process, a cache left
// truncated by a killed one, and one Ensure.
//
// A same-process test cannot show this. tursogo's InitLibrary is a sync.Once
// that panics on failure, so a process gets one attempt and then never touches
// disk again — a second Ensure in the same process returns the first result and
// repairs nothing. The failure it guards against is also cross-process by
// construction: two writers corrupt a shared file, and the survivor (or the
// next boot) is the one that heals it.
func TestRepairHealsACorruptedCacheAcrossProcesses(t *testing.T) {
	if Ineffective() {
		t.Skip("this platform has nothing to extract")
	}
	root := t.TempDir()
	env := append(os.Environ(), "TURSO_GO_CACHE_DIR="+root)

	// 1. Cold extraction, in a child process, so this test's own sync.Once
	// stays untouched.
	run := func() (string, error) {
		// #nosec G204,G702 -- os.Args[0] is this test binary; every argument is a
		// constant literal, and the env is this test's own configuration.
		cmd := exec.CommandContext(t.Context(), os.Args[0], "-test.run=TestHelperEnsure", "-test.v=false")
		cmd.Env = append(append([]string{}, env...), helperEnv+"=1")
		out, err := cmd.CombinedOutput()
		return string(out), err
	}
	if out, err := run(); err != nil || !strings.Contains(out, "ENSURE_OK") {
		t.Fatalf("cold run did not succeed: %v\n%s", err, out)
	}
	libs, _ := filepath.Glob(filepath.Join(root, "turso-go", "*", "*"))
	if len(libs) == 0 {
		t.Skip("driver extracted nothing to corrupt")
	}

	// 2. Truncate it the way a SIGKILL mid-copy does.
	if err := os.WriteFile(libs[0], []byte("TRUNCATED"), 0o600); err != nil {
		t.Fatal(err)
	}
	before := len(libs)

	// 3. A fresh process must heal it and come up clean.
	out, err := run()
	if err != nil || !strings.Contains(out, "ENSURE_OK") {
		t.Fatalf("repair did not heal the cache: %v\n%s", err, out)
	}
	// The child's own measurement must agree: one whole library, not a file it
	// merely stopped complaining about.
	reported, warned := parseEnsureReport(t, out)
	if reported < 1<<20 {
		t.Errorf("child reports %d bytes of library after repair — that is not the real library", reported)
	}
	if warned != before {
		t.Errorf("child reports %d file(s), want the %d a full extraction produces", warned, before)
	}

	// 4. And the file must be whole again, not merely absent.
	libs2, _ := filepath.Glob(filepath.Join(root, "turso-go", "*", "*"))
	if len(libs2) != before {
		t.Fatalf("expected %d extracted file(s) after repair, got %d", before, len(libs2))
	}
	info, statErr := os.Stat(libs2[0])
	if statErr != nil {
		t.Fatal(statErr)
	}
	if info.Size() < 1<<20 {
		t.Errorf("repaired library is %d bytes — that is not the real library", info.Size())
	}
}

// parseEnsureReport reads the helper's `ENSURE_OK files=N bytes=M` line, so the
// assertions live in the parent and the child stays a plain loader.
func parseEnsureReport(t *testing.T, out string) (bytes int64, files int) {
	t.Helper()
	for line := range strings.SplitSeq(out, "\n") {
		if !strings.HasPrefix(line, "ENSURE_OK") {
			continue
		}
		if _, err := fmt.Sscanf(line, "ENSURE_OK files=%d bytes=%d", &files, &bytes); err != nil {
			t.Fatalf("unreadable helper report %q: %v", line, err)
		}
		return bytes, files
	}
	t.Fatalf("helper printed no ENSURE_OK line:\n%s", out)
	return 0, 0
}
