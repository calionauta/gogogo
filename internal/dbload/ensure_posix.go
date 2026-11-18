//go:build !windows && !android

package dbload

// Drivers that embed a native library (tursogo today) write it to disk before
// dlopen. Everything here exists because that write is not atomic; see the
// package doc.

import (
	"fmt"
	"os"
	"path/filepath"

	turso_libs "github.com/tursodatabase/turso-go-platform-libs"
	"golang.org/x/sys/unix"
)

// ineffective is false: this build links a driver that extracts a library.
const ineffective = false

func loadNativeLibrary() error {
	_, err := turso_libs.LoadTursoLibrary(turso_libs.LoadTursoLibraryConfig{})
	if err != nil {
		return fmt.Errorf("dbload: load the native library: %w", err)
	}
	return nil
}

// cacheDir is the directory the loader extracts into: TURSO_GO_CACHE_DIR, then
// the user cache dir, then temp — the loader's own resolution order.
func cacheDir() string {
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

// lock takes the cross-process lock that serializes extraction.
//
// A per-process warm-up does not help: both processes warm up at the same
// instant, which is the race. flock on a file BESIDE the cache directory — not
// inside it — so purgeCache can unlink the library while the lock is held.
func lock() (unlock func(), err error) {
	root := filepath.Dir(cacheDir())
	if mkErr := os.MkdirAll(root, cacheDirPerm); mkErr != nil { // #nosec G703 -- a cache dir this process chose
		return nil, fmt.Errorf("dbload: create cache root %q: %w", root, mkErr)
	}
	lockPath := filepath.Join(root, ".turso-go.lock")
	f, err := os.OpenFile(lockPath, os.O_CREATE|os.O_RDWR, cacheFilePerm) // #nosec G703 -- constant filename
	if err != nil {
		return nil, fmt.Errorf("dbload: open lock %q: %w", lockPath, err)
	}
	if lockErr := unix.Flock(int(f.Fd()), unix.LOCK_EX); lockErr != nil {
		_ = f.Close()
		return nil, fmt.Errorf("dbload: lock %q: %w", lockPath, lockErr)
	}
	return func() {
		_ = unix.Flock(int(f.Fd()), unix.LOCK_UN)
		_ = f.Close()
	}, nil
}
