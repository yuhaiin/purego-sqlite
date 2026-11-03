//go:build !linux && !darwin && !windows && !freebsd && !openbsd && !netbsd && !dragonfly

package loader

func systemPaths() []string { return nil }
