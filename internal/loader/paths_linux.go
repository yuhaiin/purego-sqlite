//go:build linux

package loader

import "runtime"

func systemPaths() []string {
	triplet := "x86_64-linux-gnu"
	if runtime.GOARCH == "arm64" {
		triplet = "aarch64-linux-gnu"
	}
	return []string{"/usr/lib/" + triplet + "/libsqlite3.so.0", "/lib/" + triplet + "/libsqlite3.so.0", "/usr/lib64/libsqlite3.so.0", "/usr/lib/libsqlite3.so.0", "/lib64/libsqlite3.so.0", "/lib/libsqlite3.so.0"}
}
