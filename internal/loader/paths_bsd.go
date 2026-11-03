//go:build freebsd || openbsd || netbsd || dragonfly

package loader

func systemPaths() []string {
	return []string{"/usr/local/lib/libsqlite3.so.0", "/usr/local/lib/libsqlite3.so", "/usr/pkg/lib/libsqlite3.so.0", "/usr/pkg/lib/libsqlite3.so", "/usr/lib/libsqlite3.so.0", "/usr/lib/libsqlite3.so"}
}
