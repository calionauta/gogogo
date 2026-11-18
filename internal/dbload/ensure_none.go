//go:build android

package dbload

// Android links ncruces: pure-Go SQLite, no native library on disk, nothing to
// extract or serialize.

const ineffective = true

func loadNativeLibrary() error { return nil }
func cacheDir() string         { return "" }
func lock() (func(), error)    { return func() {}, nil }
