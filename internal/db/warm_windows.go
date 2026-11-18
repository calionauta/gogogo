//go:build !android && windows

// See warm_other.go for why this exists. Windows needs its own locking
// primitive: no flock, and the platform-libs module embeds libs/windows_amd64
// too, so the same non-atomic extraction runs there.

package db

import (
	"fmt"
	"os"
	"path/filepath"
	"strings"

	turso_libs "github.com/tursodatabase/turso-go-platform-libs"
	"golang.org/x/sys/windows"
)

func warmDriver() error {
	root := os.Getenv("TURSO_GO_CACHE_DIR")
	if root == "" {
		cache, err := os.UserCacheDir()
		if err != nil {
			return nil
		}
		root = cache
	}
	dir := filepath.Join(root, "turso-go")
	if err := os.MkdirAll(dir, warmDirPerm); err != nil { // #nosec G703 -- a cache dir this process chose
		return nil
	}
	lockPath := filepath.Join(dir, ".warmup.lock")
	// #nosec G703 -- constant filename under the cache dir chosen above.
	f, err := os.OpenFile(lockPath, os.O_CREATE|os.O_RDWR, warmFilePerm)
	if err != nil {
		return nil
	}
	defer f.Close()
	ol := new(windows.Overlapped)
	if err := windows.LockFileEx(windows.Handle(f.Fd()), windows.LOCKFILE_EXCLUSIVE_LOCK, 0, 1, 0, ol); err != nil {
		return nil
	}
	defer func() { _ = windows.UnlockFileEx(windows.Handle(f.Fd()), 0, 1, 0, ol) }()

	if _, err := turso_libs.LoadTursoLibrary(turso_libs.LoadTursoLibraryConfig{}); err != nil {
		// The one error worth a sentence here: tursogo's extraction is not
		// atomic, so a killed process can leave a truncated library that every
		// later boot rejects with a bare hash comparison. Without this, the
		// operator reads a hash mismatch and has no idea the fix is "delete a
		// cache directory". Any other failure passes through as-is — guessing
		// at causes we cannot identify would be worse than the raw error.
		if strings.Contains(err.Error(), "hash sum mismatch") {
			return fmt.Errorf("db: load the turso native library: %w\n\n"+
				"a hash mismatch on a file that already exists means a previous\n"+
				"extraction was interrupted; delete %s and restart", err, cacheDirForMessage())
		}
		return fmt.Errorf("db: load the turso native library: %w", err)
	}
	return nil
}
