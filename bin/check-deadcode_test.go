// SCOPE:core — contract test for the deadcode pre-push guard.

package bin_test

import (
	"errors"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"testing"
)

// The deadcode scan commits ~3.3 GB RSS (measured) and once held a push for
// 23 minutes on a saturated 4-core box. The fix is a capacity gate inside the
// script, and these tests pin its CONTRACT, because a gate that silently
// stops gating is worse than none: the push looks fine until it wedges again.
//
// Each case runs the real script with a threshold forced through the
// environment, so no host measurement is involved and the assertions are
// about behaviour, not about this machine.

func scriptPath(t *testing.T) string {
	t.Helper()
	wd, err := os.Getwd()
	if err != nil {
		t.Fatalf("getwd: %v", err)
	}
	return filepath.Join(wd, "check-deadcode.sh")
}

// runScript executes the guard with extra env and returns combined output.
func runScript(t *testing.T, extraEnv ...string) (string, int) {
	t.Helper()
	// #nosec G204 -- both arguments are test-owned: a literal "bash" and a
	// path derived from this package's own working directory.
	cmd := exec.CommandContext(t.Context(), "bash", scriptPath(t))
	cmd.Env = append(os.Environ(), extraEnv...)
	out, err := cmd.CombinedOutput()
	code := 0
	if exitErr, ok := errors.AsType[*exec.ExitError](err); ok {
		code = exitErr.ExitCode()
	} else if err != nil {
		t.Fatalf("run script: %v", err)
	}
	return string(out), code
}

// TestDeadcodeGuardSkipsOnLowRAM pins the RAM ceiling: a box that cannot hold
// the measured peak must SKIP, loudly, and must not fail the push.
func TestDeadcodeGuardSkipsOnLowRAM(t *testing.T) {
	out, code := runScript(t, "DEADCODE_MIN_RAM_MB=999999")
	if code != 0 {
		t.Fatalf("exit = %d, want 0 (the guard is advisory; it must never block a push)\n%s", code, out)
	}
	if !strings.Contains(out, "SKIPPED") {
		t.Fatalf("expected a SKIPPED verdict on a low-RAM box, got:\n%s", out)
	}
	if !strings.Contains(out, "RAM:") {
		t.Errorf("the skip must name the failed dimension (RAM), got:\n%s", out)
	}
	if !strings.Contains(out, "push proceeds") {
		t.Errorf("the skip must say the push is not blocked, got:\n%s", out)
	}
}

// TestDeadcodeGuardSkipsOnSaturatedLoad pins the load ceiling — the dimension
// that actually wedged the push. A near-zero ceiling forces the branch on any
// machine, including an idle CI runner.
func TestDeadcodeGuardSkipsOnSaturatedLoad(t *testing.T) {
	out, code := runScript(t, "DEADCODE_MAX_LOAD_PER_CORE=0.000001")
	if code != 0 {
		t.Fatalf("exit = %d, want 0\n%s", code, out)
	}
	if !strings.Contains(out, "SKIPPED") {
		t.Fatalf("expected a SKIPPED verdict on a saturated box, got:\n%s", out)
	}
	if !strings.Contains(out, "load:") {
		t.Errorf("the skip must name the failed dimension (load), got:\n%s", out)
	}
}

// TestDeadcodeGuardSkipNamesTheOverride pins discoverability: whoever hits the
// skip must learn how to run the scan anyway, without reading the source.
func TestDeadcodeGuardSkipNamesTheOverride(t *testing.T) {
	out, _ := runScript(t, "DEADCODE_MIN_RAM_MB=999999")
	if !strings.Contains(out, "DEADCODE_FORCE=1") {
		t.Errorf("the skip must name DEADCODE_FORCE=1 as the override, got:\n%s", out)
	}
}

// TestDeadcodeGuardAloneNeverFails pins the invariant that makes this safe at
// pre-push: whatever the host, whatever deadcode reports, the hook exits 0.
// A gate that can fail a push for an advisory scan is the bug this replaced.
func TestDeadcodeGuardAloneNeverFails(t *testing.T) {
	if _, code := runScript(t, "DEADCODE_MAX_LOAD_PER_CORE=0.000001"); code != 0 {
		t.Errorf("skip path exit = %d, want 0", code)
	}
	// The forced path actually runs the scan; it must still end at 0 even
	// when it reports findings (dead code exists in this tree by design).
	if testing.Short() {
		return
	}
	out, code := runScript(t, "DEADCODE_FORCE=1", "DEADCODE_MIN_RAM_MB=1",
		"DEADCODE_MAX_LOAD_PER_CORE=99999")
	if code != 0 {
		t.Errorf("forced run exit = %d, want 0 (advisory only)\n%s", code, out)
	}
	if !strings.Contains(out, "advisory only") {
		t.Errorf("the completed run must say it was advisory, got:\n%s", out)
	}
}
