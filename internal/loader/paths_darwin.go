//go:build darwin

package loader

func systemPaths() []string { return []string{"/usr/lib/libsqlite3.dylib"} }
