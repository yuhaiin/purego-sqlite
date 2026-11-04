package vendoredsqlite

import (
	"archive/zip"
	"bytes"
	"crypto/sha256"
	"crypto/sha3"
	"encoding/json"
	"fmt"
	"os"
	"path/filepath"
	"testing"

	"github.com/yuhaiin/purego-sqlite/internal/sqlitesource"
)

func futureArchive(t *testing.T, omit string, wrongVersion bool) ([]byte, sqlitesource.Pin) {
	t.Helper()
	var b bytes.Buffer
	z := zip.NewWriter(&b)
	for _, name := range sqlitesource.Files {
		if name == omit {
			continue
		}
		f, err := z.Create("sqlite-amalgamation-3550000/" + name)
		if err != nil {
			t.Fatal(err)
		}
		version := "3.55.0"
		if wrongVersion && name == "sqlite3.c" {
			version = "3.54.1"
		}
		if _, err := fmt.Fprintf(f, "#define SQLITE_VERSION %q\n", version); err != nil {
			t.Fatal(err)
		}
	}
	if err := z.Close(); err != nil {
		t.Fatal(err)
	}
	data := b.Bytes()
	return data, sqlitesource.Pin{Version: "3.55.0", URL: "https://www.sqlite.org/2026/sqlite-amalgamation-3550000.zip", SHA256: fmt.Sprintf("%x", sha256.Sum256(data)), SHA3: fmt.Sprintf("%x", sha3.Sum256(data))}
}

func prepareRoot(t *testing.T) string {
	t.Helper()
	root := t.TempDir()
	if err := sqlitesource.WriteFile(filepath.Join(root, "internal/sqlite3/native.go"), []byte("package sqlite3\n\nconst Version = \"3.54.1\"\n")); err != nil {
		t.Fatal(err)
	}
	return root
}

func TestImportFutureReleaseAndVerifyIntegrity(t *testing.T) {
	root := prepareRoot(t)
	data, pin := futureArchive(t, "", false)
	if err := Import(root, pin, data); err != nil {
		t.Fatal(err)
	}
	if err := Verify(root, pin); err != nil {
		t.Fatal(err)
	}
	for _, name := range sqlitesource.Files {
		b, err := os.ReadFile(filepath.Join(root, "internal/sqlite3", name))
		if err != nil || !bytes.Contains(b, []byte("3.55.0")) {
			t.Fatalf("%s %v", name, err)
		}
	}
	native, err := os.ReadFile(filepath.Join(root, "internal/sqlite3/native.go"))
	if err != nil || !bytes.Contains(native, []byte(`const Version = "3.55.0"`)) {
		t.Fatalf("%s %v", native, err)
	}
	manifestPath := filepath.Join(root, "internal/sqlite3/source.json")
	b, err := os.ReadFile(manifestPath)
	if err != nil {
		t.Fatal(err)
	}
	var m manifest
	if err := json.Unmarshal(b, &m); err != nil {
		t.Fatal(err)
	}
	delete(m.Files, "sqlite3ext.h")
	if err := sqlitesource.WriteJSON(manifestPath, m); err != nil {
		t.Fatal(err)
	}
	if err := Verify(root, pin); err == nil {
		t.Fatal("accepted incomplete file manifest")
	}
}

func TestInvalidArchiveDoesNotReplaceAnySource(t *testing.T) {
	for _, test := range []struct {
		name, omit     string
		wrong, corrupt bool
	}{{"missing-header", "sqlite3ext.h", false, false}, {"wrong-version", "", true, false}, {"checksum", "", false, true}} {
		t.Run(test.name, func(t *testing.T) {
			root := prepareRoot(t)
			data, pin := futureArchive(t, test.omit, test.wrong)
			if test.corrupt {
				data = append(data, 'x')
			}
			if err := Import(root, pin, data); err == nil {
				t.Fatal("invalid archive accepted")
			}
			for _, name := range sqlitesource.Files {
				if _, err := os.Stat(filepath.Join(root, "internal/sqlite3", name)); !os.IsNotExist(err) {
					t.Fatalf("partially replaced %s", name)
				}
			}
			b, err := os.ReadFile(filepath.Join(root, "internal/sqlite3/native.go"))
			if err != nil || !bytes.Contains(b, []byte("3.54.1")) {
				t.Fatal("changed runtime version on failure")
			}
		})
	}
}
