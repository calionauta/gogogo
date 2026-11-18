//go:build !windows

// Package tursotest serializes warming tursogo's native-library cache across
// test binaries. It is test infrastructure, not part of the library's API.
package tursotest

import (
	"os"
	"path/filepath"

	turso_libs "github.com/tursodatabase/turso-go-platform-libs"
	"golang.org/x/sys/unix"
)

// Warm extracts tursogo's native library ONCE, under an exclusive cross-process
// lock, and must be called from a package's TestMain before m.Run().
//
// The race it prevents. tursogo extracts the library from its embedded copy
// into a shared cache (<UserCacheDir>/turso-go/<hash8>/) on first use, and the
// extraction is not atomic: os.Create (truncate in place), copy, THEN compare
// the hash it just wrote. Any two initializers that overlap both see a file
// that exists and is non-empty, both read it while the other is mid-copy, and
// both fail with
//
//	cached library file hash sum mismatch …
//	  expected=<embedded>, got=<garbage>
//
// "got" differs every run because it samples two writers interleaved.
// Measured on this module: 48 concurrent loads on an empty cache failed 5–15 of
// 48; three test binaries started together on a cold cache failed 4 of 6
// rounds.
//
// Why a cross-process lock and not just a warm-up. `go test ./...` compiles one
// binary per package and runs them concurrently. `credits`, `payments` and
// `stripe` all import tursogo, so three separate processes hit a cold cache at
// the same instant — a per-process warm-up alone does not help, because they
// warm up simultaneously. flock on a file beside the cache directory serializes
// them across processes.
//
// The failure this prevents reads like a flaky credential test: in CI it
// surfaced as TestCredentialStoreRoundTrip panicking on a cold runner, with
// `.cache` reporting "Cache is not found".
//
// This is the test-side half only. An app importing tursogo has the same race
// if it opens several databases concurrently on a cold cache; the real fix is
// upstream (write a temp file, then rename).
func Warm() error {
	unlock, err := lock()
	if err != nil {
		return err
	}
	defer unlock()
	_, err = turso_libs.LoadTursoLibrary(turso_libs.LoadTursoLibraryConfig{})
	return err
}

// CacheDir returns the directory tursogo uses for its extracted library,
// resolved the same way the library resolves it.
func CacheDir() string {
	root := os.Getenv("TURSO_GO_CACHE_DIR")
	if root == "" {
		if d, err := os.UserCacheDir(); err == nil {
			root = d
		} else {
			root = os.TempDir()
		}
	}
	return filepath.Join(root, "turso-go")
}

// Modes the library itself uses for its cache directory and files.
const (
	cacheDirPerm  = 0o755
	cacheFilePerm = 0o644
)

func lock() (unlock func(), err error) {
	dir := CacheDir()
	// #nosec G703 -- a cache dir this process chose, not request input.
	if mkErr := os.MkdirAll(dir, cacheDirPerm); mkErr != nil {
		return nil, mkErr
	}
	lockPath := filepath.Join(dir, ".warmup.lock") // #nosec G703 -- constant filename under the dir above
	f, openErr := os.OpenFile(lockPath, os.O_CREATE|os.O_RDWR, cacheFilePerm)
	if openErr != nil {
		return nil, openErr
	}
	if lockErr := unix.Flock(int(f.Fd()), unix.LOCK_EX); lockErr != nil {
		_ = f.Close()
		return nil, lockErr
	}
	return func() {
		_ = unix.Flock(int(f.Fd()), unix.LOCK_UN)
		_ = f.Close()
	}, nil
}
