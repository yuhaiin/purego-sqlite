//go:build !linux && !darwin && !windows

package loader

func systemPaths() []string { return nil }
