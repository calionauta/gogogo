// SCOPE:core - DO NOT REMOVE - Module root package.
// Package gogogofullstacktemplate is the module root.
//
// This file exists so the module root package is never empty: some Go
// toolchains treat an empty root package as a hard "no Go files" error
// when running `go test ./...`, which aborts the whole test gate. With
// this file present the root reports "[no test files]" instead.
//
// The package name intentionally has no underscore: golangci-lint's
// revive linter rejects underscore-separated package names, and the
// module import path (github.com/calionauta/gogogo)
// is independent of the package name, so renaming is safe.
package gogogofullstacktemplate
