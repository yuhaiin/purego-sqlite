// Package loader loads libraries only from explicit or fixed system paths.
package loader

import (
	"errors"
	"fmt"
	"path/filepath"
	"runtime"
	"strings"
)

func Open(path string) (uintptr, string, error) {
	if !backendAvailable {
		return 0, "", fmt.Errorf("sqlite: no FFI backend for %s/%s; non-PureGo targets require CGO_ENABLED=1 and a supported C toolchain", runtime.GOOS, runtime.GOARCH)
	}
	if path == "" {
		if h, name := linkedLibrary(); h != 0 {
			return h, name, nil
		}
	}
	paths := systemPaths()
	if path != "" {
		if !filepath.IsAbs(path) || strings.ContainsRune(path, 0) {
			return 0, "", errors.New("sqlite: library path must be absolute and contain no NUL")
		}
		paths = []string{filepath.Clean(path)}
	}
	var errs []error
	for _, p := range paths {
		h, err := open(p)
		if err == nil {
			return h, p, nil
		}
		errs = append(errs, fmt.Errorf("%s: %w", p, err))
	}
	if len(paths) == 0 {
		return 0, "", errors.New("sqlite: no system library on this platform; use WithLibrary with an absolute path or WithBundled")
	}
	return 0, "", fmt.Errorf("sqlite: load library: %w", errors.Join(errs...))
}

// Linked libraries use a reserved handle and need no dynamic loader or unload.
const linkedHandle = ^uintptr(0)

func Symbol(handle uintptr, name string) (uintptr, error) {
	if handle == linkedHandle {
		return linkedSymbol(name)
	}
	return symbol(handle, name)
}

func Close(handle uintptr) error {
	if handle == linkedHandle {
		return nil
	}
	return closeLibrary(handle)
}
