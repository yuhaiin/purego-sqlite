//go:build (!cgo || (!linux && !darwin && !windows && !freebsd && !openbsd && !netbsd && !dragonfly)) && (sqlite_cgo || (!linux && !darwin && !windows) || (!amd64 && !arm64))

package ffi

func registerAPI(*API, any, string, uintptr) { panic("unreachable: backend unavailable") }
