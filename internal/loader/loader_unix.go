//go:build !windows && (linux || darwin || windows || freebsd || netbsd) && (amd64 || arm64) && (!cgo || sqlite_purego) && !sqlite_cgo

package loader

import "github.com/ebitengine/purego"

func open(path string) (uintptr, error) {
	return purego.Dlopen(path, purego.RTLD_NOW|purego.RTLD_LOCAL)
}
func symbol(handle uintptr, name string) (uintptr, error) { return purego.Dlsym(handle, name) }
func closeLibrary(handle uintptr) error                   { return purego.Dlclose(handle) }
