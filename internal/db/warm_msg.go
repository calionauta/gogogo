package db

import (
	"os"
	"path/filepath"
)

// cacheDirForMessage names the directory tursogo extracts into, for the one
// error whose fix is "delete it". Resolution order matches the library's:
// TURSO_GO_CACHE_DIR, then the user cache dir, then temp.
func cacheDirForMessage() string {
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
