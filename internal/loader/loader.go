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
	if (runtime.GOARCH != "amd64" && runtime.GOARCH != "arm64") || (runtime.GOOS != "linux" && runtime.GOOS != "darwin" && runtime.GOOS != "windows") {
		return 0, "", fmt.Errorf("sqlite: unsupported platform %s/%s", runtime.GOOS, runtime.GOARCH)
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
