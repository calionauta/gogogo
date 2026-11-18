//go:build windows

package dbload

// See ensure_posix.go: same problem, different primitive (Windows has no flock).

import (
	"fmt"
	"os"
	"path/filepath"

	turso_libs "github.com/tursodatabase/turso-go-platform-libs"
	"golang.org/x/sys/windows"
)

const ineffective = false

func loadNativeLibrary() error {
	_, err := turso_libs.LoadTursoLibrary(turso_libs.LoadTursoLibraryConfig{})
	if err != nil {
		return fmt.Errorf("dbload: load the native library: %w", err)
	}
	return nil
}

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
	// LOCKFILE_EXCLUSIVE_LOCK without FAIL_IMMEDIATELY blocks, which is what
	// serializes the two processes.
	ol := new(windows.Overlapped)
	if err := windows.LockFileEx(windows.Handle(f.Fd()), windows.LOCKFILE_EXCLUSIVE_LOCK, 0, 1, 0, ol); err != nil {
		_ = f.Close()
		return nil, fmt.Errorf("dbload: lock %q: %w", lockPath, err)
	}
	return func() {
		_ = windows.UnlockFileEx(windows.Handle(f.Fd()), 0, 1, 0, ol)
		_ = f.Close()
	}, nil
}
