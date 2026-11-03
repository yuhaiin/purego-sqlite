//go:build (darwin && arm64) || (!linux && !darwin && !windows) || (!amd64 && !arm64)

package ffi

func extensionConfig(uintptr) func(uintptr, int32) int32 { return nil }
