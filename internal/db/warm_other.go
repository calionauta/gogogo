//go:build !android && !windows

// tursogo extracts its native library from an embedded copy into a shared cache
// on first use, and that extraction is not atomic: os.Create (truncate in
// place), copy, THEN compare the hash it just wrote. Two processes that start
// together both see a file that exists and is non-empty, both read it
// mid-copy, and both die with
//
//	cached library file hash sum mismatch … got=<garbage>
//
// where "got" is a snapshot of two writers interleaved, so it differs per run.
//
// The test suite hit this constantly (one binary per package, run
// concurrently — see internal/tursotest). Production hits it only if two
// processes share a cache on a cold machine: `make dev` plus `gogogo recipe
// install`, or a second deploy slot starting before the first finished. Rare,
// but the failure is a total startup failure with an error that names a hash
// rather than the real cause, and the guard costs one flock at boot.

package db

import (
	"fmt"
	"os"
	"path/filepath"

	turso_libs "github.com/tursodatabase/turso-go-platform-libs"
	"golang.org/x/sys/unix"
)

// warmDriver extracts the native library once under a cross-process lock.
func warmDriver() error {
	root := os.Getenv("TURSO_GO_CACHE_DIR")
	if root == "" {
		cache, err := os.UserCacheDir()
		if err != nil {
			// No cache dir: let the library pick its own fallback and resolve
			// its own errors. Not being able to lock is not worth failing boot.
			return nil
		}
		root = cache
	}
	dir := filepath.Join(root, "turso-go")
	// #nosec G703 -- a cache directory this process chose, not request input.
	if err := os.MkdirAll(dir, warmDirPerm); err != nil {
		return nil // same reasoning: the library reports its own extraction errors
	}
	lockPath := filepath.Join(dir, ".warmup.lock")
	// #nosec G703 -- constant filename under the cache dir chosen above.
	f, err := os.OpenFile(lockPath, os.O_CREATE|os.O_RDWR, warmFilePerm)
	if err != nil {
		return nil
	}
	defer f.Close()
	if err := unix.Flock(int(f.Fd()), unix.LOCK_EX); err != nil {
		return nil // unlocked is still better than not starting
	}
	defer func() { _ = unix.Flock(int(f.Fd()), unix.LOCK_UN) }()

	if _, err := turso_libs.LoadTursoLibrary(turso_libs.LoadTursoLibraryConfig{}); err != nil {
		return fmt.Errorf("db: load the turso native library: %w", err)
	}
	return nil
}
