package sqlite

import (
	"context"
	"errors"
	"os"
	"path/filepath"
	"strings"
	"sync"
	"testing"
	"time"
)

func requireFeature(t *testing.T, name string) {
	t.Helper()
	c := testConnector(t, ":memory:")
	ok, err := c.HasFeature(name)
	if err != nil {
		t.Fatal(err)
	}
	if !ok {
		if os.Getenv("SQLITE_REQUIRE_FEATURES") == "1" {
			t.Fatalf("required bundled feature %s missing", name)
		}
		t.Skipf("SQLite %s lacks optional feature %s", c.Version(), name)
	}
}

func TestFTS5(t *testing.T) {
	requireFeature(t, "fts5")
	db := testDB(t, WithWAL())
	exec(t, db, `CREATE VIRTUAL TABLE documents USING fts5(title,content,tokenize='unicode61')`)
	exec(t, db, `INSERT INTO documents(title,content) VALUES(?,?)`, "SQLite", "PureGo FFI SQLite driver café 中文 日本語")
	exec(t, db, `INSERT INTO documents(title,content) VALUES(?,?)`, "Large", strings.Repeat("native sqlite text ", 10000))
	for _, query := range []string{"SQLite", `"PureGo FFI"`, "driv*", "cafe", "中文", "日本語"} {
		var n int
		if err := db.QueryRow("SELECT count(*) FROM documents WHERE documents MATCH ?", query).Scan(&n); err != nil || n < 1 {
			t.Fatalf("MATCH %q: %d %v", query, n, err)
		}
	}
	var title, highlight, snippet string
	var rank float64
	if err := db.QueryRow(`SELECT title,bm25(documents),highlight(documents,1,'[',']'),snippet(documents,1,'[',']','...',12) FROM documents WHERE documents MATCH 'PureGo' ORDER BY bm25(documents)`).Scan(&title, &rank, &highlight, &snippet); err != nil {
		t.Fatal(err)
	}
	if title != "SQLite" || rank >= 0 || !strings.Contains(highlight, "[PureGo]") || !strings.Contains(snippet, "[PureGo]") {
		t.Fatalf("ranking/snippets: %s %f %s %s", title, rank, highlight, snippet)
	}
	tx, err := db.Begin()
	if err != nil {
		t.Fatal(err)
	}
	exec(t, tx, "DELETE FROM documents WHERE title='SQLite'")
	if err := tx.Rollback(); err != nil {
		t.Fatal(err)
	}
	var n int
	if err := db.QueryRow("SELECT count(*) FROM documents WHERE documents MATCH 'PureGo'").Scan(&n); err != nil || n != 1 {
		t.Fatal(n, err)
	}
	exec(t, db, "DELETE FROM documents WHERE title='Large'")
	exec(t, db, "INSERT INTO documents(documents) VALUES('integrity-check')")
	integrity(t, db)
}
func TestJSONAndRTree(t *testing.T) {
	t.Run("JSON", func(t *testing.T) {
		requireFeature(t, "json")
		db := testDB(t)
		var got string
		if err := db.QueryRow(`SELECT json_extract(json_set(?, '$.x', 42), '$.x')`, `{"x":1}`).Scan(&got); err != nil || got != "42" {
			t.Fatal(got, err)
		}
		c := testConnector(t, ":memory:")
		ok, err := c.HasFeature("jsonb")
		if err != nil {
			t.Fatal(err)
		}
		if !ok {
			if os.Getenv("SQLITE_REQUIRE_FEATURES") == "1" {
				t.Fatal("bundle lacks JSONB")
			}
			return
		}
		if err := db.QueryRow(`SELECT json_extract(jsonb(?),'$.hello')`, `{"hello":"world"}`).Scan(&got); err != nil || got != "world" {
			t.Fatal(got, err)
		}
	})
	t.Run("RTree", func(t *testing.T) {
		requireFeature(t, "rtree")
		db := testDB(t)
		exec(t, db, "CREATE VIRTUAL TABLE boxes USING rtree(id,minX,maxX,minY,maxY)")
		exec(t, db, "INSERT INTO boxes VALUES(1,0,10,0,10)")
		var id int
		if err := db.QueryRow("SELECT id FROM boxes WHERE minX<=5 AND maxX>=5 AND minY<=5 AND maxY>=5").Scan(&id); err != nil || id != 1 {
			t.Fatal(id, err)
		}
		integrity(t, db)
	})
}

func TestExtensions(t *testing.T) {
	path := os.Getenv("SQLITE_TEST_EXTENSION")
	if path == "" {
		t.Skip("set SQLITE_TEST_EXTENSION to locally compiled tests/testdata/extension.c")
	}
	c := testConnector(t, ":memory:")
	capable, err := c.HasFeature("extensions")
	if err != nil {
		t.Fatal(err)
	}
	if !capable {
		if _, err := NewConnector(":memory:", WithLibrary(c.LibraryPath()), WithExtensions()); !errors.Is(err, ErrUnsupported) {
			t.Fatal(err)
		}
		t.Skip("secure extension ABI unavailable on this platform/library")
	}
	path, err = filepath.Abs(path)
	if err != nil {
		t.Fatal(err)
	}
	db := testDB(t)
	conn, err := db.Conn(t.Context())
	if err != nil {
		t.Fatal(err)
	}
	if err := LoadExtension(t.Context(), conn, path, ""); !errors.Is(err, ErrExtensionDisabled) {
		t.Fatal(err)
	}
	conn.Close()
	db2 := testDB(t, WithExtensions(), WithInit(func(ctx context.Context, c *Conn) error { return c.LoadExtension(ctx, path, "") }))
	db2.SetMaxOpenConns(4)
	var wg sync.WaitGroup
	for range 4 {
		wg.Go(func() {
			conn, err := db2.Conn(t.Context())
			if err != nil {
				t.Error(err)
				return
			}
			defer conn.Close()
			for range 20 {
				var n int
				if err := conn.QueryRowContext(t.Context(), "SELECT test_twice(21)").Scan(&n); err != nil || n != 42 {
					t.Error(n, err)
				}
			}
			// SQL-level loading must remain disabled before, during and after C loading.
			if _, err := conn.ExecContext(t.Context(), "SELECT load_extension(?)", path); err == nil || !strings.Contains(err.Error(), "not authorized") {
				t.Errorf("SQL extension loading enabled: %v", err)
			}
		})
	}
	wg.Wait()
	conn, err = db2.Conn(t.Context())
	if err != nil {
		t.Fatal(err)
	}
	defer conn.Close()
	for range 2 {
		if err := LoadExtension(t.Context(), conn, path, ""); err != nil {
			t.Fatal(err)
		}
	}
	for _, entry := range []string{"not_an_entry_point", "bad\x00entry"} {
		if err := LoadExtension(t.Context(), conn, path, entry); err == nil {
			t.Fatal("accepted invalid entry", entry)
		}
	}
	if err := LoadExtension(t.Context(), conn, path+".missing", ""); err == nil {
		t.Fatal("missing extension accepted")
	}
	if err := LoadExtension(t.Context(), conn, "relative.so", ""); err == nil {
		t.Fatal("relative extension accepted")
	}
	invalid := filepath.Join(t.TempDir(), "invalid.so")
	if err := os.WriteFile(invalid, []byte("not a library"), 0600); err != nil {
		t.Fatal(err)
	}
	if err := LoadExtension(t.Context(), conn, invalid, ""); err == nil {
		t.Fatal("invalid library accepted")
	}
	var n int
	if err := conn.QueryRowContext(t.Context(), "SELECT test_twice(4)").Scan(&n); err != nil || n != 8 {
		t.Fatal(n, err)
	}
	if _, err := conn.ExecContext(t.Context(), "SELECT load_extension(?)", path); err == nil {
		t.Fatal("SQL loading enabled after failure")
	}
}

func TestSchemaReprepare(t *testing.T) {
	db := testDB(t)
	exec(t, db, "CREATE TABLE t(a)")
	exec(t, db, "INSERT INTO t VALUES(1)")
	s, err := db.Prepare("SELECT * FROM t")
	if err != nil {
		t.Fatal(err)
	}
	defer s.Close()
	exec(t, db, "ALTER TABLE t ADD COLUMN b DEFAULT 2")
	r, err := s.Query()
	if err != nil {
		t.Fatal(err)
	}
	defer r.Close()
	cols, err := r.Columns()
	if err != nil || len(cols) != 2 {
		t.Fatalf("reprepare columns: %v %v", cols, err)
	}
	if !r.Next() {
		t.Fatal(r.Err())
	}
	var a, b int
	if err := r.Scan(&a, &b); err != nil || a != 1 || b != 2 {
		t.Fatal(a, b, err)
	}
}

func TestRollbackOnConflictAndCommitBusy(t *testing.T) {
	db := testDB(t, WithBusyTimeout(10*time.Millisecond))
	db.SetMaxOpenConns(2)
	exec(t, db, "CREATE TABLE t(a UNIQUE)")
	tx, err := db.Begin()
	if err != nil {
		t.Fatal(err)
	}
	exec(t, tx, "INSERT INTO t VALUES(1)")
	if _, err := tx.Exec("INSERT OR ROLLBACK INTO t VALUES(1)"); !errors.Is(err, ErrConstraint) {
		t.Fatal(err)
	}
	if err := tx.Commit(); err == nil {
		t.Fatal("native rollback not reflected")
	}
	exec(t, db, "INSERT INTO t VALUES(2)")
	reader, err := db.Conn(t.Context())
	if err != nil {
		t.Fatal(err)
	}
	r, err := reader.QueryContext(t.Context(), "SELECT * FROM t")
	if err != nil {
		t.Fatal(err)
	}
	if !r.Next() {
		t.Fatal(r.Err())
	}
	tx, err = db.Begin()
	if err != nil {
		t.Fatal(err)
	}
	exec(t, tx, "INSERT INTO t VALUES(3)")
	if err := tx.Commit(); !errors.Is(err, ErrBusy) {
		t.Fatal(err)
	}
	r.Close()
	reader.Close()
	var count int
	if err := db.QueryRow("SELECT count(*) FROM t").Scan(&count); err != nil || count != 1 {
		t.Fatal(count, err)
	}
	integrity(t, db)
}
