package db

// Modes for the cache directory and lock file, matching what tursogo uses for
// its own extracted library.
const (
	warmDirPerm  = 0o755
	warmFilePerm = 0o644
)
