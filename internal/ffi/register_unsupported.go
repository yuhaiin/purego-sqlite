//go:build !((linux || darwin || windows || freebsd || netbsd) && (amd64 || arm64) && (!cgo || sqlite_purego) && !sqlite_cgo) && !(cgo && !sqlite_purego && (linux || darwin || windows || freebsd || openbsd || netbsd || dragonfly))

package ffi

func registerAPI(*API, any, string, uintptr) { panic("unreachable: backend unavailable") }
