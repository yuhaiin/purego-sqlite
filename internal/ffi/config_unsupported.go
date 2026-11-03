//go:build ((!cgo || (!linux && !darwin && !windows && !freebsd && !openbsd && !netbsd && !dragonfly)) && (sqlite_cgo || (!linux && !darwin && !windows) || (!amd64 && !arm64))) || (darwin && arm64 && !sqlite_cgo)

package ffi

func extensionConfig(uintptr) func(uintptr, int32) int32 { return nil }
