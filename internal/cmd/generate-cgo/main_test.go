package main

import (
	"bytes"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/yuhaiin/purego-sqlite/internal/sqlitesource"
)

func TestGeneratedTrampolinesMatchCommittedABI(t *testing.T) {
	root := t.TempDir()
	api, err := os.ReadFile("../../ffi/api.go")
	if err != nil {
		t.Fatal(err)
	}
	if err := sqlitesource.WriteFile(filepath.Join(root, "internal/ffi/api.go"), api); err != nil {
		t.Fatal(err)
	}
	if err := generate(root); err != nil {
		t.Fatal(err)
	}
	for _, name := range []string{"internal/ffi/register_cgo.go", "internal/sqlite3/symbols.h", "internal/ffi/callback_slots.h"} {
		got, err := os.ReadFile(filepath.Join(root, name))
		if err != nil {
			t.Fatal(err)
		}
		want, err := os.ReadFile(filepath.Join("../../..", name))
		if err != nil {
			t.Fatal(err)
		}
		want = bytes.ReplaceAll(want, []byte("\r\n"), []byte("\n"))
		if !bytes.Equal(got, want) {
			t.Fatalf("generated ABI changed: %s", name)
		}
	}
	api = []byte(strings.Replace(string(api), "Open                func", "UnknownAPI          func", 1))
	if err := sqlitesource.WriteFile(filepath.Join(root, "internal/ffi/api.go"), api); err != nil {
		t.Fatal(err)
	}
	if err := generate(root); err == nil {
		t.Fatal("new API field accepted without a native C signature")
	}
}
