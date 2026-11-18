//go:build android

package db

// warmDriver is a no-op on Android: this platform uses the ncruces driver,
// which has no extracted native library to race over.
func warmDriver() error { return nil }
