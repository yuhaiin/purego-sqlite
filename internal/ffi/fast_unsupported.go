//go:build sqlite_cgo || (!linux && !darwin && !windows) || (!amd64 && !arm64)

package ffi

func bindFast(*API, uintptr) {}
