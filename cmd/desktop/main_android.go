//go:build android

package main

import "github.com/wailsapp/wails/v3/pkg/application"

// Android loads the Go code as a shared library, so main() is not called
// automatically. Register it with the Wails bridge; the overlay generator
// targets the project root, but this binary's main package is cmd/desktop.
func init() {
	application.RegisterAndroidMain(main)
}
