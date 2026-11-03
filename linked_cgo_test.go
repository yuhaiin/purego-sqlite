//go:build cgo && !sqlite_purego && !sqlite_dynamic && (linux || darwin || windows || freebsd || openbsd || netbsd || dragonfly)

package sqlite

import (
	"context"
	"strings"
	"testing"
)

func TestDefaultCGOUsesLinkedEngine(t *testing.T) {
	connector, err := NewConnector(":memory:")
	if err != nil {
		t.Fatal(err)
	}
	if !strings.HasPrefix(connector.LibraryPath(), "builtin:sqlite3-") {
		t.Fatal("default CGO engine was not linked", connector.LibraryPath())
	}
	for _, feature := range []string{"fts5", "json", "jsonb", "rtree", "math", "dbstat"} {
		ok, err := connector.HasFeature(feature)
		if err != nil || !ok {
			t.Fatal(feature, ok, err)
		}
	}
	connection, err := connector.Connect(context.Background())
	if err != nil {
		t.Fatal(err)
	}
	if err := connection.Close(); err != nil {
		t.Fatal(err)
	}
	// An explicit library request must never silently fall back to the engine.
	if _, err := NewConnector(":memory:", WithLibrary(t.TempDir()+"/missing.sqlite")); err == nil {
		t.Fatal("ignored explicit library selection")
	}
}
