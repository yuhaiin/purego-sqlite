//go:build linux

package loader

import "runtime"

func systemPaths() []string {
	triplet := map[string]string{
		"amd64": "x86_64-linux-gnu", "arm64": "aarch64-linux-gnu",
		"386": "i386-linux-gnu", "arm": "arm-linux-gnueabihf",
		"mips": "mips-linux-gnu", "mipsle": "mipsel-linux-gnu",
		"mips64": "mips64-linux-gnuabi64", "mips64le": "mips64el-linux-gnuabi64",
		"ppc64": "powerpc64-linux-gnu", "ppc64le": "powerpc64le-linux-gnu",
		"riscv64": "riscv64-linux-gnu", "s390x": "s390x-linux-gnu",
		"loong64": "loongarch64-linux-gnu",
	}[runtime.GOARCH]
	if triplet == "" {
		return []string{"/usr/lib/libsqlite3.so.0", "/lib/libsqlite3.so.0"}
	}
	return []string{"/usr/lib/" + triplet + "/libsqlite3.so.0", "/lib/" + triplet + "/libsqlite3.so.0", "/usr/lib64/libsqlite3.so.0", "/usr/lib/libsqlite3.so.0", "/lib64/libsqlite3.so.0", "/lib/libsqlite3.so.0"}
}
