// Package dbload serializes loading a SQLite driver's native library.
//
// # The problem it exists for
//
// A driver that ships its native library *embedded* must write it to disk before
// dlopen, and tursogo does that with os.Create (truncate in place) → copy →
// THEN compare the hash it just wrote. Not atomic, no lock. Two processes that
// start together both see a file that exists and non-empty, both read it
// mid-copy, and both die with
//
//	cached library file hash sum mismatch … got=<garbage>
//
// where "got" is a snapshot of two writers interleaved, so it differs per run.
// A process killed during that write leaves a truncated file that every later
// boot rejects, forever, until someone deletes it by hand.
//
// # What this package provides
//
//	Ensure()         load the library once, serialized across processes, and
//	                 repair a cache left half-written by an interrupted run.
//	PrepareWorkspace() the same for the ledger file a caller is about to open —
//	                 the one call a caller makes.
//	Ineffective()    true when nothing is embedded to race over (ncruces).
//
// # Platform split
//
//	ensure_posix.go    flock; used by every driver that embeds a library
//	ensure_windows.go  LockFileEx (Windows has no flock)
//	ensure_none.go     the no-op, for drivers with nothing to extract
//
// # One implementation, for product and tests alike
//
// There is no test-side copy. A _test.go file imports this package like any
// other (hundreds already import the product tree), so a second copy would be
// two implementations to keep in step — and the first attempt at this guard
// shipped exactly that mistake, then nearly shipped a repair that deleted
// nothing because the copy nobody exercised had the bug.
package dbload

import (
	"fmt"
	"os"
	"path/filepath"
	"strings"
)

// Ineffective reports whether this build loads no embedded native library, so
// Ensure and PrepareWorkspace have nothing to do.
//
// Android uses the ncruces driver: pure Go, nothing to extract. Keeping the
// predicate here — rather than only in a build-tagged file — lets a test on any
// platform assert the Android answer, which is the one that must not regress
// into "no-op because nobody remembered".
func Ineffective() bool { return ineffective }

// Ensure loads the driver's embedded native library, once, serialized across
// processes, repairing a cache left half-written by an interrupted run.
//
// It is one call and it is cheap after the first boot (a hash of a file that is
// already there), so lifecycle code calls it without consulting Ineffective:
// that way a forgotten build tag cannot silently disable the guard.
func Ensure() error {
	if Ineffective() {
		return nil
	}
	unlock, err := lock()
	if err != nil {
		return err
	}
	defer unlock()
	return verifyOrRepair(loadNativeLibrary, func() error { return purgeCache(cacheDir()) })
}

// PrepareWorkspace runs Ensure and then creates the parent directory of the
// given database file.
//
// It exists because SQLite creates a database's directory lazily and its
// error for "no such directory" is indistinguishable from the rarer failures a
// later migration would report; failing at the open, named, is better. It also
// keeps the ordering in one place: the native library must be loaded before the
// first connection.
func PrepareWorkspace(dbPath string) error {
	if err := Ensure(); err != nil {
		return err
	}
	dir := filepath.Dir(dbPath)
	if dir == "" || dir == "." {
		return nil
	}
	if err := os.MkdirAll(dir, dirPerm); err != nil {
		return fmt.Errorf("dbload: create data dir %q: %w", dir, err)
	}
	return nil
}

// corruptedCacheHint is appended when a cached library had to be discarded.
//
// It exists because the natural reading of a hash mismatch is "the embedded
// library is wrong", and the operator goes looking at the binary. The cause is
// almost always a killed process mid-write, and the cure is deleting one
// generated file — which Ensure now does unasked, but a log line saying so is
// what turns a mystery into a known event.
const corruptedCacheHint = "a previous extraction was interrupted; the cache was discarded and rebuilt"

// verifyOrRepair loads the library, deleting a stale cache first when the only
// complaint is a hash mismatch.
//
// Deleting is safe by construction, not by hope: the file is generated from the
// binary's own embedded copy, so the worst case of a wrong delete is one extra
// extraction. That is also why the retry is bounded — one repairable error, one
// rebuild — instead of a loop that could spin on a genuinely bad library.
func verifyOrRepair(load func() error, purge func() error) error {
	err := load()
	if err == nil {
		return nil
	}
	if !isCacheCorruption(err) {
		return err
	}
	if purgeErr := purge(); purgeErr != nil {
		// Report both: the load failure is the one that stops the process, and
		// the purge failure says why the repair could not happen.
		return fmt.Errorf("%w (discarding the cache also failed: %w)", err, purgeErr)
	}
	if retryErr := load(); retryErr != nil {
		return fmt.Errorf("%w (%s, and rebuilding it failed: %w)", retryErr, corruptedCacheHint, retryErr)
	}
	return nil
}

// isCacheCorruption reports whether err is the loader complaining about a
// half-written cache file rather than, say, a missing platform library. The
// match is on the loader's own wording; if it is ever reworded this degrades to
// reporting the original error, which is the pre-repair behaviour.
func isCacheCorruption(err error) bool {
	return strings.Contains(err.Error(), "hash sum mismatch")
}

// purgeCache deletes the loader's whole cache tree, leaving the lock file in
// place.
//
// The tree, not just its files: the loader extracts into a hash-named
// SUBdirectory of this one (`turso-go/<hash8>/lib…`), so removing only the top
// level's files leaves the corrupt library exactly where it was — a repair that
// reports success and changes nothing. Re-created by the loader on the next
// load, which is what makes deleting the directory safe.
//
// The lock lives OUTSIDE this tree precisely so this can run while holding it:
// unlinking a locked file would drop the lock out from under the other waiters.
func purgeCache(dir string) error {
	if err := os.RemoveAll(dir); err != nil && !os.IsNotExist(err) {
		return err
	}
	return nil
}
