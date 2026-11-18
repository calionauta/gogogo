//go:build windows

package tursotest

import (
	"os"
	"path/filepath"

	turso_libs "github.com/tursodatabase/turso-go-platform-libs"
	"golang.org/x/sys/windows"
)

// Warm extracts tursogo's native library ONCE, under an exclusive cross-process
// lock. See tursotest.go for the race this prevents — same bug on Windows:
// the platform-libs module embeds libs/windows_amd64 too, and the extraction is
// the same non-atomic os.Create/copy/verify sequence.
//
// Windows needs its own locking primitive (no flock), hence this file. A named
// mutex would also work, but a lock file keyed to the cache directory keeps the
// mechanism identical to the POSIX side and does not depend on a session-local
// name.
func Warm() error {
	unlock, err := lock()
	if err != nil {
		return err
	}
	defer unlock()
	_, err = turso_libs.LoadTursoLibrary(turso_libs.LoadTursoLibraryConfig{})
	return err
}

// CacheDir returns the directory tursogo uses for its extracted library.
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

func lock() (unlock func(), err error) {
	dir := CacheDir()
	if mkErr := os.MkdirAll(dir, cacheDirPerm); mkErr != nil { // #nosec G703 -- a cache dir this process chose, not request input
		return nil, mkErr
	}
	lockPath := filepath.Join(dir, ".warmup.lock") // #nosec G703 -- constant filename under the dir above
	f, openErr := os.OpenFile(lockPath, os.O_CREATE|os.O_RDWR, cacheFilePerm)
	if openErr != nil {
		return nil, openErr
	}
	// LOCKFILE_EXCLUSIVE_LOCK blocks; without FAIL_IMMEDIATELY LockFileEx waits,
	// which is what serializes the two processes.
	ol := new(windows.Overlapped)
	if lockErr := windows.LockFileEx(windows.Handle(f.Fd()), windows.LOCKFILE_EXCLUSIVE_LOCK, 0, 1, 0, ol); lockErr != nil {
		_ = f.Close()
		return nil, lockErr
	}
	return func() {
		_ = windows.UnlockFileEx(windows.Handle(f.Fd()), 0, 1, 0, ol)
		_ = f.Close()
	}, nil
}
