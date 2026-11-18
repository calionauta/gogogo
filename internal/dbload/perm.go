package dbload

// Modes for the cache root and the lock file beside it.
const (
	cacheDirPerm  = 0o755
	cacheFilePerm = 0o644

	// dirPerm is the mode for a database's parent directory, created on first
	// boot. Group/other read+execute lets backups and sidecars inspect data/
	// without write access; the database file keeps the process umask.
	dirPerm = 0o755
)
