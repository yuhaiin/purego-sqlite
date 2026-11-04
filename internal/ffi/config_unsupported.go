//go:build (!((linux || darwin || windows || freebsd || netbsd) && (amd64 || arm64) && (!cgo || sqlite_purego) && !sqlite_cgo) && !(cgo && !sqlite_purego && (linux || darwin || windows || freebsd || openbsd || netbsd || dragonfly))) || (darwin && arm64 && (!cgo || sqlite_purego) && !sqlite_cgo)

package ffi

func extensionConfig(uintptr) func(uintptr, int32) int32 { return nil }
