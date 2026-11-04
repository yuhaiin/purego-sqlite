package benchmarks

import (
	"bytes"
	"context"
	"database/sql"
	"fmt"
	"net/url"
	"os"
	"path/filepath"
	"runtime"
	"slices"
	"strings"
	"testing"

	_ "github.com/mattn/go-sqlite3"
	sqlite "github.com/yuhaiin/purego-sqlite"
	_ "modernc.org/sqlite"
)

var drivers = []string{backendName, "mattn", "modernc"}

func open(b testing.TB, implementation, path string, connections int) *sql.DB {
	b.Helper()
	var db *sql.DB
	if implementation == backendName {
		var opts []sqlite.Option
		if lib := os.Getenv("BENCH_SQLITE_LIBRARY"); lib != "" {
			opts = append(opts, sqlite.WithLibrary(lib))
		}
		opts = append(opts, sqlite.WithInit(func(ctx context.Context, c *sqlite.Conn) error {
			for _, q := range []string{"PRAGMA synchronous=NORMAL", "PRAGMA cache_size=-2000", "PRAGMA busy_timeout=5000"} {
				if _, err := c.ExecContext(ctx, q, nil); err != nil {
					return err
				}
			}
			return nil
		}))
		c, err := sqlite.NewConnector(path, opts...)
		if err != nil {
			b.Fatal(err)
		}
		db = sql.OpenDB(c)
	} else if implementation == "mattn" {
		// mattn applies these options to EVERY physical connection.
		var err error
		db, err = sql.Open("sqlite3", "file:"+filepath.ToSlash(path)+"?_synchronous=NORMAL&_cache_size=-2000&_busy_timeout=5000")
		if err != nil {
			b.Fatal(err)
		}
	} else if implementation == "modernc" {
		// _pragma is applied to every physical connection by modernc.
		params := url.Values{"_pragma": {"synchronous(NORMAL)", "cache_size(-2000)", "busy_timeout(5000)"}}
		var err error
		db, err = sql.Open("sqlite", "file:"+filepath.ToSlash(path)+"?"+params.Encode())
		if err != nil {
			b.Fatal(err)
		}
	} else {
		b.Fatalf("unknown implementation %q", implementation)
	}
	db.SetMaxOpenConns(connections)
	db.SetMaxIdleConns(connections)
	if err := db.Ping(); err != nil {
		b.Fatal(err)
	}
	return db
}
func exec(b testing.TB, db *sql.DB, q string, args ...any) {
	b.Helper()
	if _, err := db.Exec(q, args...); err != nil {
		b.Fatal(err)
	}
}
func setup(b *testing.B, implementation string, connections int) *sql.DB {
	b.Helper()
	db := open(b, implementation, filepath.Join(b.TempDir(), "bench.db"), connections)
	b.Cleanup(func() {
		if err := db.Close(); err != nil {
			b.Error(err)
		}
	})
	var journal string
	if err := db.QueryRow("PRAGMA journal_mode=WAL").Scan(&journal); err != nil || journal != "wal" {
		b.Fatal(journal, err)
	}
	exec(b, db, "CREATE TABLE items(id INTEGER PRIMARY KEY, name TEXT, value INTEGER)")
	exec(b, db, "CREATE INDEX items_name ON items(name)")
	exec(b, db, "CREATE TABLE events(id INTEGER PRIMARY KEY, value INTEGER)")
	tx, err := db.Begin()
	if err != nil {
		b.Fatal(err)
	}
	s, err := tx.Prepare("INSERT INTO items VALUES(?,?,?)")
	if err != nil {
		b.Fatal(err)
	}
	for i := range 1000 {
		if _, err := s.Exec(i, fmt.Sprintf("name-%04d", i), i); err != nil {
			b.Fatal(err)
		}
	}
	if err := s.Close(); err != nil {
		b.Fatal(err)
	}
	if err := tx.Commit(); err != nil {
		b.Fatal(err)
	}
	b.ReportAllocs()
	return db
}
func throughput(b *testing.B) {
	b.StopTimer()
	b.ReportMetric(float64(b.N)/b.Elapsed().Seconds(), "ops/s")
}

func BenchmarkSQLite(b *testing.B) {
	cases := []struct {
		name string
		run  func(*testing.B, string)
	}{
		{"OpenClose", benchOpenClose}, {"SelectConstant", benchConstant},
		{"SingleInsert", benchInsert}, {"PreparedInsert", benchPreparedInsert},
		{"BatchInsert1000", func(b *testing.B, d string) { benchBatch(b, d, 1000) }},
		{"BatchInsert1000Values", benchBatchValues},
		{"TransactionInsert100000", func(b *testing.B, d string) { benchBatch(b, d, 100000) }},
		{"SingleRowSelect", benchSingleRow}, {"FullScan1000", benchScan}, {"IndexedLookup", benchIndexed},
		{"LargeTextRead", func(b *testing.B, d string) { benchPayload(b, d, false, false) }},
		{"LargeTextWrite", func(b *testing.B, d string) { benchPayload(b, d, false, true) }},
		{"LargeBlobRead", func(b *testing.B, d string) { benchPayload(b, d, true, false) }},
		{"LargeBlobWrite", func(b *testing.B, d string) { benchPayload(b, d, true, true) }},
		{"WALReaders", benchReaders}, {"WALWriterContention", benchWriters},
		{"FTS5Search", benchFTS5}, {"JSONQuery", benchJSON},
	}
	for _, c := range cases {
		b.Run(c.name, func(b *testing.B) {
			for _, d := range drivers {
				b.Run(d, func(b *testing.B) { c.run(b, d) })
			}
		})
	}
}
func benchOpenClose(b *testing.B, d string) {
	path := filepath.Join(b.TempDir(), "open.db")
	db := open(b, d, path, 1)
	exec(b, db, "CREATE TABLE t(x)")
	db.Close()
	b.ReportAllocs()
	for b.Loop() {
		db := open(b, d, path, 1)
		if err := db.Close(); err != nil {
			b.Fatal(err)
		}
	}
	throughput(b)
}
func benchConstant(b *testing.B, d string) {
	db := setup(b, d, 1)
	for b.Loop() {
		var n int
		if err := db.QueryRow("SELECT 1").Scan(&n); err != nil || n != 1 {
			b.Fatal(n, err)
		}
	}
	throughput(b)
}
func benchInsert(b *testing.B, d string) {
	db := setup(b, d, 1)
	for b.Loop() {
		exec(b, db, "INSERT INTO events(value) VALUES(?)", 42)
	}
	throughput(b)
}
func benchPreparedInsert(b *testing.B, d string) {
	db := setup(b, d, 1)
	s, err := db.Prepare("INSERT INTO events(value) VALUES(?)")
	if err != nil {
		b.Fatal(err)
	}
	defer s.Close()
	for b.Loop() {
		if _, err := s.Exec(42); err != nil {
			b.Fatal(err)
		}
	}
	throughput(b)
}
func benchBatch(b *testing.B, d string, n int) {
	db := setup(b, d, 1)
	for b.Loop() {
		b.StopTimer()
		exec(b, db, "DELETE FROM events")
		b.StartTimer()
		tx, err := db.Begin()
		if err != nil {
			b.Fatal(err)
		}
		s, err := tx.Prepare("INSERT INTO events(value) VALUES(?)")
		if err != nil {
			b.Fatal(err)
		}
		for i := range n {
			if _, err := s.Exec(i); err != nil {
				b.Fatal(err)
			}
		}
		if err := s.Close(); err != nil {
			b.Fatal(err)
		}
		if err := tx.Commit(); err != nil {
			b.Fatal(err)
		}
	}
	b.ReportMetric(float64(n), "rows/op")
	throughput(b)
}

// Keep the row-at-a-time transaction case above. This separately measures a
// common batched SQL shape: 1000 bindings, one Step/result/reset per transaction.
func benchBatchValues(b *testing.B, d string) {
	db := setup(b, d, 1)
	query := "INSERT INTO events(value) VALUES" + strings.TrimSuffix(strings.Repeat("(?),", 1000), ",")
	args := make([]any, 1000)
	for i := range args {
		args[i] = int64(i)
	}
	for b.Loop() {
		b.StopTimer()
		exec(b, db, "DELETE FROM events")
		b.StartTimer()
		tx, err := db.Begin()
		if err != nil {
			b.Fatal(err)
		}
		s, err := tx.Prepare(query)
		if err != nil {
			b.Fatal(err)
		}
		result, err := s.Exec(args...)
		if err != nil {
			b.Fatal(err)
		}
		if n, err := result.RowsAffected(); err != nil || n != 1000 {
			b.Fatal(n, err)
		}
		if err := s.Close(); err != nil {
			b.Fatal(err)
		}
		if err := tx.Commit(); err != nil {
			b.Fatal(err)
		}
	}
	b.ReportMetric(1000, "rows/op")
	throughput(b)
}
func benchSingleRow(b *testing.B, d string) {
	db := setup(b, d, 1)
	for b.Loop() {
		var name string
		var value int
		if err := db.QueryRow("SELECT name,value FROM items WHERE id=?", 500).Scan(&name, &value); err != nil || value != 500 {
			b.Fatal(value, err)
		}
	}
	throughput(b)
}
func benchIndexed(b *testing.B, d string) {
	db := setup(b, d, 1)
	for b.Loop() {
		var id int
		if err := db.QueryRow("SELECT id FROM items WHERE name=?", "name-0500").Scan(&id); err != nil || id != 500 {
			b.Fatal(id, err)
		}
	}
	throughput(b)
}
func benchScan(b *testing.B, d string) {
	db := setup(b, d, 1)
	for b.Loop() {
		rows, err := db.Query("SELECT id,name,value FROM items")
		if err != nil {
			b.Fatal(err)
		}
		count := 0
		for rows.Next() {
			var id, value int
			var name string
			if err := rows.Scan(&id, &name, &value); err != nil {
				b.Fatal(err)
			}
			count++
		}
		if err := rows.Err(); err != nil {
			b.Fatal(err)
		}
		rows.Close()
		if count != 1000 {
			b.Fatal(count)
		}
	}
	b.ReportMetric(1000, "rows/op")
	throughput(b)
}
func benchPayload(b *testing.B, d string, blob, write bool) {
	db := setup(b, d, 1)
	exec(b, db, "CREATE TABLE payload(id INTEGER PRIMARY KEY,value)")
	data := bytes.Repeat([]byte("SQLite🌍\x00"), (1<<20)/len("SQLite🌍\x00"))
	var value any = string(data)
	if blob {
		value = data
	}
	exec(b, db, "INSERT INTO payload VALUES(1,?)", value)
	b.SetBytes(int64(len(data)))
	if write {
		s, err := db.Prepare("UPDATE payload SET value=? WHERE id=1")
		if err != nil {
			b.Fatal(err)
		}
		defer s.Close()
		for b.Loop() {
			if _, err := s.Exec(value); err != nil {
				b.Fatal(err)
			}
		}
	} else {
		for b.Loop() {
			if blob {
				var v []byte
				if err := db.QueryRow("SELECT value FROM payload WHERE id=1").Scan(&v); err != nil || len(v) != len(data) {
					b.Fatal(len(v), err)
				}
			} else {
				var v string
				if err := db.QueryRow("SELECT value FROM payload WHERE id=1").Scan(&v); err != nil || len(v) != len(data) {
					b.Fatal(len(v), err)
				}
			}
		}
	}
	throughput(b)
}
func benchReaders(b *testing.B, d string) {
	db := setup(b, d, 4)
	b.ResetTimer()
	b.RunParallel(func(pb *testing.PB) {
		for pb.Next() {
			var value int
			if err := db.QueryRow("SELECT value FROM items WHERE id=500").Scan(&value); err != nil || value != 500 {
				b.Error(value, err)
				return
			}
		}
	})
	throughput(b)
}
func benchWriters(b *testing.B, d string) {
	db := setup(b, d, 4)
	b.ResetTimer()
	b.RunParallel(func(pb *testing.PB) {
		for pb.Next() {
			if _, err := db.Exec("UPDATE items SET value=value+1 WHERE id=500"); err != nil {
				b.Error(err)
				return
			}
		}
	})
	throughput(b)
}
func benchFTS5(b *testing.B, d string) {
	db := setup(b, d, 1)
	exec(b, db, "CREATE VIRTUAL TABLE documents USING fts5(body)")
	exec(b, db, `INSERT INTO documents(body) SELECT 'native sqlite purego ' || name FROM items`)
	for b.Loop() {
		var n int
		if err := db.QueryRow("SELECT count(*) FROM documents WHERE documents MATCH ?", "sqlite").Scan(&n); err != nil || n != 1000 {
			b.Fatal(n, err)
		}
	}
	throughput(b)
}
func benchJSON(b *testing.B, d string) {
	db := setup(b, d, 1)
	for b.Loop() {
		var n int
		if err := db.QueryRow(`SELECT json_extract(?, '$.value')`, `{"value":42,"data":[1,2,3]}`).Scan(&n); err != nil || n != 42 {
			b.Fatal(n, err)
		}
	}
	throughput(b)
}

// Never compare two different SQLite engines/configurations by accident.
func TestSameNativeSQLite(t *testing.T) {
	var nativeOptions []sqlite.Option
	if path := os.Getenv("BENCH_SQLITE_LIBRARY"); path != "" {
		nativeOptions = append(nativeOptions, sqlite.WithLibrary(path))
	}
	connector, err := sqlite.NewConnector(":memory:", nativeOptions...)
	if err != nil {
		t.Fatal(err)
	}
	rowReader, err := connector.HasFeature("row_read_v1")
	if err != nil {
		t.Fatal(err)
	}
	scalarExec, err := connector.HasFeature("exec_scalar_v1")
	if err != nil {
		t.Fatal(err)
	}
	t.Logf("native library=%s row_read_v1=%v exec_scalar_v1=%v", connector.LibraryPath(), rowReader, scalarExec)
	var identity, options string
	for _, d := range drivers {
		db := open(t, d, filepath.Join(t.TempDir(), "identity.db"), 1)
		var version, source string
		if err := db.QueryRow("SELECT sqlite_version(),sqlite_source_id()").Scan(&version, &source); err != nil {
			t.Fatal(err)
		}
		r, err := db.Query("PRAGMA compile_options")
		if err != nil {
			t.Fatal(err)
		}
		var opts []string
		for r.Next() {
			var s string
			if err := r.Scan(&s); err != nil {
				t.Fatal(err)
			}
			opts = append(opts, s)
		}
		if err := r.Err(); err != nil {
			t.Fatal(err)
		}
		r.Close()
		slices.Sort(opts)
		current := strings.Join(opts, ",")
		if d != "modernc" && identity != "" && (identity != source || options != current) {
			t.Fatal("SQLite source/options differ; run with -tags libsqlite3 and the same shared library")
		}
		if d != "modernc" {
			identity, options = source, current
		}
		t.Logf("driver=%s Go=%s SQLite=%s source=%s compile_options=%s", d, runtime.Version(), version, source, current)
		db.Close()
	}
}

// Check the settings on all four simultaneously live physical connections,
// including those created after WAL was enabled on the first connection.
func TestConnectionSettings(t *testing.T) {
	for _, d := range drivers {
		t.Run(d, func(t *testing.T) {
			db := open(t, d, filepath.Join(t.TempDir(), "settings.db"), 4)
			defer db.Close()
			exec(t, db, "PRAGMA journal_mode=WAL")
			for range 4 {
				c, err := db.Conn(t.Context())
				if err != nil {
					t.Fatal(err)
				}
				defer c.Close()
				for pragma, want := range map[string]int{"synchronous": 1, "cache_size": -2000, "busy_timeout": 5000} {
					var got int
					if err := c.QueryRowContext(t.Context(), "PRAGMA "+pragma).Scan(&got); err != nil || got != want {
						t.Fatalf("%s=%d, want %d: %v", pragma, got, want, err)
					}
				}
				var journal string
				if err := c.QueryRowContext(t.Context(), "PRAGMA journal_mode").Scan(&journal); err != nil || journal != "wal" {
					t.Fatal(journal, err)
				}
			}
		})
	}
}
