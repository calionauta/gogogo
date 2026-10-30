//go:build android

// This file exists only to enable cgo for the package on Android, so the
// bcmp shim in bcmp_android.c is compiled and linked into libwails.so.
package main

import "C"
