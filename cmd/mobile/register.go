//go:build android

package main

import "github.com/wailsapp/wails/v3/pkg/application"

// Android loads Go as a shared library, so main is not called automatically.
func init() {
	application.RegisterAndroidMain(main)
}
