//go:build !windows && !((linux || darwin || windows || freebsd || netbsd) && (amd64 || arm64) && (!cgo || sqlite_purego) && !sqlite_cgo) && !(cgo && !sqlite_purego && (linux || darwin || windows || freebsd || openbsd || netbsd || dragonfly))

package loader

import "errors"

func open(string) (uintptr, error)            { return 0, errors.New("sqlite: unsupported platform") }
func symbol(uintptr, string) (uintptr, error) { return 0, errors.New("sqlite: unsupported platform") }
func closeLibrary(uintptr) error              { return nil }
