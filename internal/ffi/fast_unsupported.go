//go:build !((linux || darwin || windows || freebsd || netbsd) && (amd64 || arm64) && (!cgo || sqlite_purego) && !sqlite_cgo)

package ffi

func bindFast(*API, uintptr) {}
