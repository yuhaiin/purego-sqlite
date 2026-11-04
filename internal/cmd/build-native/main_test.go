package main

import (
	"slices"
	"testing"
)

func TestCompilerCommandWords(t *testing.T) {
	for _, test := range []struct {
		command string
		want    []string
	}{
		{"cc", []string{"cc"}},
		{"zig cc -target aarch64-windows-gnu", []string{"zig", "cc", "-target", "aarch64-windows-gnu"}},
		{`"C:\Program Files\LLVM\bin\clang.exe" -O2`, []string{`C:\Program Files\LLVM\bin\clang.exe`, "-O2"}},
		{`'/compiler path/cc' -target mipsel-linux`, []string{"/compiler path/cc", "-target", "mipsel-linux"}},
	} {
		got, err := commandWords(test.command)
		if err != nil || !slices.Equal(got, test.want) {
			t.Fatalf("%q: %q %v", test.command, got, err)
		}
	}
	if _, err := commandWords(`"unfinished`); err == nil {
		t.Fatal("accepted unfinished compiler quote")
	}
}
