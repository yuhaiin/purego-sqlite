//go:build (!linux && !darwin && !windows) || (!amd64 && !arm64)

package loader

import "errors"

func open(string) (uintptr, error)            { return 0, errors.New("sqlite: unsupported platform") }
func Symbol(uintptr, string) (uintptr, error) { return 0, errors.New("sqlite: unsupported platform") }
func Close(uintptr) error                     { return nil }
