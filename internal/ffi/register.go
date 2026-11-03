//go:build (linux || darwin || windows) && (amd64 || arm64)

package ffi

import (
	"github.com/ebitengine/purego"
	"strings"
)

func register(fn any, symbol uintptr) { purego.RegisterFunc(fn, symbol) }
func splitSymbol(tag string) (string, bool) {
	name, _, optional := strings.Cut(tag, ",")
	return name, optional
}
