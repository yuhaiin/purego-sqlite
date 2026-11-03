//go:build (linux || darwin) && (amd64 || arm64) && !sqlite_cgo

package loader

import "github.com/ebitengine/purego"

func open(path string) (uintptr, error) {
	return purego.Dlopen(path, purego.RTLD_NOW|purego.RTLD_LOCAL)
}
func Symbol(handle uintptr, name string) (uintptr, error) { return purego.Dlsym(handle, name) }
func Close(handle uintptr) error                          { return purego.Dlclose(handle) }
