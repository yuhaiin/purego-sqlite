//go:build !(((linux && !android) || (darwin && !ios) || windows || freebsd || netbsd) && (amd64 || arm64) && (!cgo || sqlite_purego) && !sqlite_cgo) && !(cgo && !sqlite_purego && (linux || darwin || windows || freebsd || openbsd || netbsd || dragonfly))

package ffi

import "fmt"

func newCallbackPointers(*API) (CallbackPointers, error) {
	return CallbackPointers{}, fmt.Errorf("sqlite: native callbacks unavailable")
}
