package dbload

import (
	"errors"
	"os"
	"path/filepath"
	"runtime"
	"strings"
	"testing"
)

// These tests run on the host, where the linked driver is the one that embeds a
// native library (tursogo). What they pin is the CONTRACT, so that swapping the
// driver to a pure-Go one (ncruces) or to another driver with the same
// extraction bug stays a data change and not a hunt.

// TestNothingToLoadOnAndroid pins the swap this package was shaped around.
//
// Android links ncruces: pure Go, no library on disk, so there is nothing to
// extract and nothing to serialize. If that ever regresses — a driver swap that
// forgets the build-tagged files — this fails instead of the guard silently
// becoming a no-op or, worse, spinning on a lock for nothing.
func TestNothingToLoadOnAndroid(t *testing.T) {
	// ineffective is a build-time constant, so this asserts the value compiled
	// into THIS platform rather than re-deriving it: on a host it must be false,
	// and the constant is what the Android build selects.
	if runtime.GOOS == "android" {
		if !ineffective {
			t.Fatal("on android the driver is ncruces (pure Go): ineffective must be true, " +
				"otherwise we take a lock to extract a library that does not exist")
		}
		return
	}
	if ineffective {
		t.Fatal("on this platform the driver embeds a native library: ineffective must be false, " +
			"otherwise Ensure is a silent no-op and the extraction race is unguarded")
	}
}

// TestPurgeRemovesOnlyFilesAndSparesTheLock is the safety property the repair
// depends on.
//
// The lock file lives BESIDE the cache directory, not inside it, and this is
// why: purgeCache unlinks everything in the directory while the lock is held. If
// the lock were inside, deleting it would release the lock out from under the
// processes waiting on it — the repair would turn one corrupt cache into two
// racers.
func TestPurgeRemovesTheTreeAndSparesTheLock(t *testing.T) {
	root := t.TempDir()
	cache := filepath.Join(root, "turso-go")
	if err := os.MkdirAll(filepath.Join(cache, "subdir"), 0o755); err != nil {
		t.Fatal(err)
	}
	// A lock in the position the real code uses: beside the cache dir.
	lockPath := filepath.Join(root, ".turso-go.lock")
	if err := os.WriteFile(lockPath, nil, 0o600); err != nil {
		t.Fatal(err)
	}
	stale := filepath.Join(cache, "libturso.so")
	if err := os.WriteFile(stale, []byte("truncated"), 0o600); err != nil {
		t.Fatal(err)
	}

	if err := purgeCache(cache); err != nil {
		t.Fatalf("purgeCache: %v", err)
	}

	if _, err := os.Stat(stale); !os.IsNotExist(err) {
		t.Fatal("the stale library survived: the repair would never rebuild it")
	}
	if _, err := os.Stat(lockPath); err != nil {
		t.Fatalf("the lock file was deleted (%v): waiting processes would lose their lock "+
			"and race the rebuild it is meant to serialize", err)
	}
	// The tree goes: the loader extracts into a hash-named SUBdirectory of it,
	// so a file-only purge silently leaves the corrupt library in place.
	if _, err := os.Stat(cache); !os.IsNotExist(err) {
		t.Fatal("the cache tree survived: the loader extracts into a subdirectory of it, " +
			"so the stale library would still be the one loaded")
	}
}

// TestMissingCacheIsNotAnError keeps the repair idempotent. Boot can run twice
// (a retry, a test that calls it after cleanup) and the second call must not
// invent a failure.
func TestMissingCacheIsNotAnError(t *testing.T) {
	if err := purgeCache(filepath.Join(t.TempDir(), "never-created")); err != nil {
		t.Fatalf("purging a directory that does not exist must be a no-op: %v", err)
	}
}

// TestRepairRebuildsAfterCorruption is the end-to-end behaviour the operator
// gets: a cache left half-written by a killed process heals at the next boot
// instead of failing forever.
//
// RED-proofed: with the purge arm of verifyOrRepair removed, the second load
// keeps failing and this test reports the mismatch it was written for.
func TestRepairRebuildsAfterCorruption(t *testing.T) {
	loads := 0
	load := func() error {
		loads++
		if loads == 1 {
			return &corruptCacheError{}
		}
		return nil
	}
	purged := false
	purge := func() error { purged = true; return nil }

	if err := verifyOrRepair(load, purge); err != nil {
		t.Fatalf("a corrupt cache must be rebuilt, got: %v", err)
	}
	if !purged {
		t.Fatal("the cache was not discarded, so the rebuild would read the same truncated file")
	}
	if loads != 2 {
		t.Fatalf("loads = %d, want 2 (fail, purge, retry): the retry is what makes the repair real", loads)
	}
}

// TestRepairDoesNotLoopOnAGenuineFailure bounds the retry. A library that is
// genuinely wrong must not be deleted and rebuilt forever.
func TestRepairDoesNotLoopOnAGenuineFailure(t *testing.T) {
	loads := 0
	load := func() error { loads++; return &corruptCacheError{} }
	purge := func() error { return nil }

	if err := verifyOrRepair(load, purge); err == nil {
		t.Fatal("a library that fails after a rebuild must report an error, not spin")
	}
	if loads != 2 {
		t.Fatalf("loads = %d, want exactly 2: one attempt, one rebuild, then report", loads)
	}
}

// TestRepairLeavesOtherFailuresAlone. Purging on an unrelated error would delete
// a good cache and, worse, hide the real cause behind a successful retry.
func TestRepairLeavesOtherFailuresAlone(t *testing.T) {
	sentinel := &otherError{}
	load := func() error { return sentinel }
	purge := func() error { t.Setenv("SHOULD_NOT", "purge was called"); return nil }

	err := verifyOrRepair(load, purge)
	if !errors.Is(err, sentinel) {
		t.Fatalf("err = %v, want the original error unchanged", err)
	}
	if os.Getenv("SHOULD_NOT") != "" {
		t.Fatal("an unrelated failure purged a good cache")
	}
}

// TestPurgeFailureIsReportedWithTheCause keeps both halves visible: the load
// failure is what stops boot, and the purge failure explains why it could not
// self-heal.
func TestPurgeFailureIsReportedWithTheCause(t *testing.T) {
	load := func() error { return &corruptCacheError{} }
	purge := func() error { return &otherError{} }

	err := verifyOrRepair(load, purge)
	if err == nil {
		t.Fatal("want an error")
	}
	msg := err.Error()
	if !strings.Contains(msg, "hash sum mismatch") || !strings.Contains(msg, "discarding the cache also failed") {
		t.Fatalf("err = %q, want it to name both the mismatch and the purge failure", msg)
	}
}

// hostError stands in for the loader's messages without importing a real
// corrupt library, so these tests assert the decision, not the driver.
type corruptCacheError struct{}

func (e *corruptCacheError) Error() string {
	return "cached library file hash sum mismatch at /x: expected=aa, got=bb"
}

type otherError struct{}

func (e *otherError) Error() string { return "the embedded library is not present for this platform" }
