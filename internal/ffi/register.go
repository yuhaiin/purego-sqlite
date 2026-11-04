//go:build (linux || darwin || windows || freebsd || netbsd) && (amd64 || arm64) && (!cgo || sqlite_purego) && !sqlite_cgo

package ffi

import (
	"github.com/ebitengine/purego"
)

func register(fn any, symbol uintptr) { purego.RegisterFunc(fn, symbol) }

func registerAPI(_ *API, fn any, _ string, symbol uintptr) { register(fn, symbol) }
