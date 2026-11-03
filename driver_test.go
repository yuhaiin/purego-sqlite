package sqlite

import (
	"bytes"
	"context"
	"database/sql"
	"database/sql/driver"
	"errors"
	"io"
	"math"
	"os"
	"path/filepath"
	"strings"
	"sync"
	"testing"
	"time"
)

func testConnector(t testing.TB, dsn string, opts ...Option) *Connector {
	t.Helper()
	if path := os.Getenv("SQLITE_TEST_LIBRARY"); path != "" {
		opts = append(opts, WithLibrary(path))
	}
	if os.Getenv("SQLITE_TEST_BUNDLED") == "1" {
		opts = append(opts, WithBundled())
	}
	c, err := NewConnector(dsn, opts...)
	if err != nil {
		t.Fatal(err)
	}
	return c
}
func testDB(t testing.TB, opts ...Option) *sql.DB {
	t.Helper()
	c := testConnector(t, filepath.Join(t.TempDir(), "test.db"), opts...)
	db := sql.OpenDB(c)
	t.Cleanup(func() {
		if err := db.Close(); err != nil {
			t.Error(err)
		}
	})
	if err := db.Ping(); err != nil {
		t.Fatal(err)
	}
	return db
}
func exec(t testing.TB, db interface {
	Exec(string, ...any) (sql.Result, error)
}, q string, args ...any) sql.Result {
	t.Helper()
	r, err := db.Exec(q, args...)
	if err != nil {
		t.Fatal(q, err)
	}
	return r
}
func integrity(t *testing.T, db *sql.DB) {
	t.Helper()
	var s string
	if err := db.QueryRow("PRAGMA integrity_check").Scan(&s); err != nil || s != "ok" {
		t.Fatalf("integrity: %q %v", s, err)
	}
}

func TestCRUDAndValues(t *testing.T) {
	db := testDB(t)
	db.SetMaxOpenConns(1)
	exec(t, db, "CREATE TABLE data(id INTEGER PRIMARY KEY, i INTEGER, r REAL, s TEXT, b BLOB, n, flag BOOLEAN)")
	s, err := db.Prepare("INSERT INTO data(i,r,s,b,n,flag) VALUES(?,?,?,?,?,?)")
	if err != nil {
		t.Fatal(err)
	}
	defer s.Close()
	text := "你好\x00SQLite🌍"
	blob := bytes.Repeat([]byte{0, 255, 128, 1}, 256*1024)
	for _, i := range []int64{math.MinInt64, math.MaxInt64} {
		r, err := s.Exec(i, math.Pi, text, blob, nil, true)
		if err != nil {
			t.Fatal(err)
		}
		id, _ := r.LastInsertId()
		affected, _ := r.RowsAffected()
		if id < 1 || affected != 1 {
			t.Fatalf("result: %d %d", id, affected)
		}
		var gotI, flag int64
		var gotR float64
		var gotText string
		var gotBlob []byte
		var n any
		if err := db.QueryRow("SELECT i,r,s,b,n,flag FROM data WHERE id=?", id).Scan(&gotI, &gotR, &gotText, &gotBlob, &n, &flag); err != nil {
			t.Fatal(err)
		}
		if gotI != i || gotR != math.Pi || gotText != text || !bytes.Equal(gotBlob, blob) || n != nil || flag != 1 {
			t.Fatal("value mismatch")
		}
	}
}

func TestNullEmptyAndRebinding(t *testing.T) {
	db := testDB(t)
	stmt, err := db.Prepare("SELECT ?, typeof(?), ?, typeof(?), ?")
	if err != nil {
		t.Fatal(err)
	}
	defer stmt.Close()
	for range 30 {
		var s, st, bt string
		var b []byte
		var n any
		if err := stmt.QueryRow("", "", []byte{}, []byte{}, []byte(nil)).Scan(&s, &st, &b, &bt, &n); err != nil {
			t.Fatal(err)
		}
		if s != "" || st != "text" || b == nil || len(b) != 0 || bt != "blob" || n != nil {
			t.Fatalf("empty/null: %q %q %#v %q %#v", s, st, b, bt, n)
		}
	}
	var got string
	if err := db.QueryRow("SELECT ?", "a\x00b").Scan(&got); err != nil || got != "a\x00b" {
		t.Fatalf("NUL: %q %v", got, err)
	}
	if _, err := db.Exec("SELECT ?", time.Now()); err == nil {
		t.Fatal("time must be explicitly encoded")
	}
}

func TestParametersAndSQLHandling(t *testing.T) {
	db := testDB(t)
	var a, b, c int
	if err := db.QueryRow("/*prefix*/ SELECT :x, @x, $y; --tail\n /*done*/", sql.Named("y", 3), sql.Named("x", 7)).Scan(&a, &b, &c); err != nil || a != 7 || b != 7 || c != 3 {
		t.Fatalf("named: %d %d %d %v", a, b, c, err)
	}
	for _, q := range []string{"SELECT 1; SELECT 2", "SELECT 1; /*x*/ SELECT bad", "SELECT 1\x00; SELECT 2"} {
		if _, err := db.Exec(q); err == nil {
			t.Fatal("accepted trailing SQL", q)
		}
	}
	for _, q := range []string{"SELECT 1; /*unterminated", "SELECT 1; --comment", "SELECT 1;;;\n"} {
		exec(t, db, q)
	}
	if _, err := db.Exec("--empty"); err == nil {
		t.Fatal("empty SQL accepted")
	}
	if _, err := db.Exec("SELECT :x", sql.Named("typo", 1)); err == nil {
		t.Fatal("unknown parameter")
	}
	if _, err := db.Exec("SELECT ?, ?", 1); err == nil {
		t.Fatal("missing parameter")
	}
	if _, err := db.Exec("SELECT ?", 1, 2); err == nil {
		t.Fatal("extra parameter")
	}
	exec(t, db, "CREATE TABLE u(id INTEGER PRIMARY KEY, name TEXT UNIQUE)")
	exec(t, db, "INSERT INTO u(name) VALUES(?)", "a")
	_, err := db.Exec("INSERT INTO u(name) VALUES(?)", "a")
	e, ok := errors.AsType[*Error](err)
	if !ok || !errors.Is(err, ErrConstraint) || e.ExtendedCode != 2067 {
		t.Fatalf("extended error: %#v", err)
	}
	exec(t, db, "INSERT INTO u(name) VALUES(?)", "b")
	integrity(t, db)
}

func TestTransactionsAndMetadata(t *testing.T) {
	db := testDB(t, WithTransactionMode("IMMEDIATE"))
	db.SetMaxOpenConns(1)
	exec(t, db, "CREATE TABLE data(id INTEGER PRIMARY KEY, name VARCHAR(100) NOT NULL)")
	tx, err := db.BeginTx(t.Context(), nil)
	if err != nil {
		t.Fatal(err)
	}
	exec(t, tx, "INSERT INTO data(name) VALUES(?)", "rollback")
	exec(t, tx, "SAVEPOINT a")
	exec(t, tx, "INSERT INTO data(name) VALUES('savepoint')")
	exec(t, tx, "ROLLBACK TO a")
	if err := tx.Rollback(); err != nil {
		t.Fatal(err)
	}
	tx, err = db.Begin()
	if err != nil {
		t.Fatal(err)
	}
	exec(t, tx, "INSERT INTO data(name) VALUES('commit')")
	if err := tx.Commit(); err != nil {
		t.Fatal(err)
	}
	rows, err := db.Query("SELECT id,name FROM data")
	if err != nil {
		t.Fatal(err)
	}
	types, err := rows.ColumnTypes()
	if err != nil || types[0].DatabaseTypeName() != "INTEGER" || types[1].DatabaseTypeName() != "VARCHAR" {
		t.Fatalf("metadata: %v %v", types, err)
	}
	if _, ok := types[1].Nullable(); ok {
		t.Fatal("nullability should be unknown")
	}
	var count int
	for rows.Next() {
		count++
	}
	if err := rows.Err(); err != nil {
		t.Fatal(err)
	}
	rows.Close()
	if count != 1 {
		t.Fatal(count)
	}
	if _, err := db.BeginTx(t.Context(), &sql.TxOptions{Isolation: sql.LevelSerializable}); !errors.Is(err, ErrUnsupported) {
		t.Fatal(err)
	}
	if _, err := db.BeginTx(t.Context(), &sql.TxOptions{ReadOnly: true}); !errors.Is(err, ErrUnsupported) {
		t.Fatal(err)
	}
	integrity(t, db)
}

const expensiveSQL = `WITH RECURSIVE n(x) AS (VALUES(0) UNION ALL SELECT x+1 FROM n WHERE x<1000000000) SELECT sum(x) FROM n`

func TestCancellationAndReuse(t *testing.T) {
	db := testDB(t)
	db.SetMaxOpenConns(1)
	for range 20 {
		ctx, cancel := context.WithTimeout(t.Context(), 5*time.Millisecond)
		var n int
		err := db.QueryRowContext(ctx, expensiveSQL).Scan(&n)
		cancel()
		if !errors.Is(err, context.DeadlineExceeded) {
			t.Fatalf("cancel: %v", err)
		}
		if err := db.QueryRow("SELECT 42").Scan(&n); err != nil || n != 42 {
			t.Fatalf("reuse: %d %v", n, err)
		}
	}
	ctx, cancel := context.WithCancel(t.Context())
	rows, err := db.QueryContext(ctx, "SELECT 1 UNION ALL SELECT 2")
	if err != nil {
		t.Fatal(err)
	}
	if !rows.Next() {
		t.Fatal(rows.Err())
	}
	cancel()
	rows.Close()
	if err := db.Ping(); err != nil {
		t.Fatal(err)
	}
}

// Force cancellation before native step: a single interrupt here is a no-op.
func TestCancellationBeforeStepAndClose(t *testing.T) {
	dc, err := testConnector(t, ":memory:").Connect(t.Context())
	if err != nil {
		t.Fatal(err)
	}
	c := dc.(*Conn)
	s, err := c.Prepare(expensiveSQL)
	if err != nil {
		t.Fatal(err)
	}
	ctx, cancel := context.WithCancel(t.Context())
	c.mu.Lock()
	err = c.runLocked(ctx, func() error {
		cancel()
		// Wait until the cancellation callback has had an opportunity to interrupt
		// an idle connection, then start step. Repeated interrupts must still stop it.
		time.Sleep(10 * time.Millisecond)
		_, err := s.(*stmt).execLocked(nil)
		return err
	})
	c.mu.Unlock()
	if !errors.Is(err, context.Canceled) {
		t.Fatal(err)
	}
	var wg sync.WaitGroup
	for range 10 {
		wg.Go(func() { _ = c.Close(); cancel(); _ = s.Close() })
	}
	wg.Wait()
}

func TestActiveRowsAndOwnership(t *testing.T) {
	dc, err := testConnector(t, ":memory:").Connect(t.Context())
	if err != nil {
		t.Fatal(err)
	}
	c := dc.(*Conn)
	defer c.Close()
	s, err := c.Prepare("SELECT ?")
	if err != nil {
		t.Fatal(err)
	}
	r, err := s.Query([]driver.Value{[]byte{1, 2, 3}})
	if err != nil {
		t.Fatal(err)
	}
	if _, err := c.ExecContext(t.Context(), "SELECT 2", nil); !errors.Is(err, ErrActiveRows) {
		t.Fatal(err)
	}
	dest := make([]driver.Value, 1)
	if err := r.Next(dest); err != nil {
		t.Fatal(err)
	}
	data := dest[0].([]byte)
	if err := s.Close(); err != nil {
		t.Fatal(err)
	}
	if !bytes.Equal(data, []byte{1, 2, 3}) {
		t.Fatal("borrowed data escaped")
	}
	if err := r.Next(dest); !errors.Is(err, io.EOF) {
		t.Fatal(err)
	}
	if err := r.Close(); err != nil {
		t.Fatal(err)
	}
	c.Close()
	if _, err := c.Prepare("SELECT 1"); !errors.Is(err, ErrClosed) {
		t.Fatal(err)
	}
}

func TestWALConcurrencyBusyAndReadOnly(t *testing.T) {
	db := testDB(t, WithWAL(), WithBusyTimeout(30*time.Millisecond))
	db.SetMaxOpenConns(6)
	exec(t, db, "CREATE TABLE counter(id INTEGER PRIMARY KEY, n INTEGER)")
	exec(t, db, "INSERT INTO counter VALUES(1,0)")
	var wg sync.WaitGroup
	for i := range 6 {
		wg.Go(func() {
			for range 30 {
				if i == 0 {
					if _, err := db.Exec("UPDATE counter SET n=n+1"); err != nil {
						t.Error(err)
					}
				} else {
					var n int
					if err := db.QueryRow("SELECT n FROM counter").Scan(&n); err != nil {
						t.Error(err)
					}
				}
			}
		})
	}
	wg.Wait()
	conn1, err := db.Conn(t.Context())
	if err != nil {
		t.Fatal(err)
	}
	defer conn1.Close()
	conn2, err := db.Conn(t.Context())
	if err != nil {
		t.Fatal(err)
	}
	defer conn2.Close()
	if _, err := conn1.ExecContext(t.Context(), "BEGIN IMMEDIATE"); err != nil {
		t.Fatal(err)
	}
	if _, err := conn2.ExecContext(t.Context(), "UPDATE counter SET n=7"); !errors.Is(err, ErrBusy) {
		t.Fatal(err)
	}
	if _, err := conn1.ExecContext(t.Context(), "ROLLBACK"); err != nil {
		t.Fatal(err)
	}
	// Native rollback makes Commit report the real state, without poisoning reuse.
	tx, err := db.Begin()
	if err != nil {
		t.Fatal(err)
	}
	exec(t, tx, "ROLLBACK")
	if err := tx.Commit(); err == nil {
		t.Fatal("commit accepted rolled back transaction")
	}
	var path string
	if err := db.QueryRow("SELECT file FROM pragma_database_list WHERE name='main'").Scan(&path); err != nil {
		t.Fatal(err)
	}
	ro := sql.OpenDB(testConnector(t, path, WithReadOnly()))
	defer ro.Close()
	if _, err := ro.Exec("UPDATE counter SET n=0"); !errors.Is(err, ErrReadOnly) {
		t.Fatal(err)
	}
	integrity(t, db)
}

func TestChurnAndInitCleanup(t *testing.T) {
	c := testConnector(t, filepath.Join(t.TempDir(), "churn.db"))
	for range 1000 {
		dc, err := c.Connect(t.Context())
		if err != nil {
			t.Fatal(err)
		}
		s, err := dc.Prepare("SELECT 1")
		if err != nil {
			t.Fatal(err)
		}
		// Closing the connection finalizes even an unclosed statement.
		if err := dc.Close(); err != nil {
			t.Fatal(err)
		}
		if err := s.Close(); err != nil {
			t.Fatal(err)
		}
	}
	fail := errors.New("init failure")
	bad := testConnector(t, ":memory:", WithInit(func(ctx context.Context, c *Conn) error { _, _ = c.Prepare("SELECT 1"); return fail }))
	if _, err := bad.Connect(t.Context()); !errors.Is(err, fail) {
		t.Fatal(err)
	}
	bad = testConnector(t, ":memory:", WithInit(func(ctx context.Context, c *Conn) error { _, err := c.Prepare("SELECT 1"); return err }))
	if _, err := bad.Connect(t.Context()); err == nil {
		t.Fatal("leaked statement in hook")
	}
	if _, err := NewConnector("bad\x00filename"); err == nil {
		t.Fatal("invalid filename")
	}
	if _, err := NewConnector(":memory:", WithLibrary("relative.so")); err == nil {
		t.Fatal("relative library")
	}
	if _, err := NewConnector(":memory:", WithTransactionMode("IMMEDIATE; DROP TABLE x")); err == nil {
		t.Fatal("unvalidated mode")
	}
}

func TestCanceledTransactionAndDBCloseRows(t *testing.T) {
	db := testDB(t)
	db.SetMaxOpenConns(1)
	exec(t, db, "CREATE TABLE t(x)")
	ctx, cancel := context.WithCancel(t.Context())
	tx, err := db.BeginTx(ctx, nil)
	if err != nil {
		t.Fatal(err)
	}
	exec(t, tx, "INSERT INTO t VALUES(1)")
	cancel()
	// database/sql's rollback goroutine eventually releases this sole connection.
	if err := db.PingContext(t.Context()); err != nil {
		t.Fatal(err)
	}
	var count int
	if err := db.QueryRow("SELECT count(*) FROM t").Scan(&count); err != nil || count != 0 {
		t.Fatalf("rollback: %d %v", count, err)
	}
	rows, err := db.Query("SELECT 'still alive'")
	if err != nil {
		t.Fatal(err)
	}
	if err := db.Close(); err != nil {
		t.Fatal(err)
	}
	if !rows.Next() {
		t.Fatal(rows.Err())
	}
	if err := rows.Close(); err != nil {
		t.Fatal(err)
	}
}

func TestDefaultRegistration(t *testing.T) {
	// The ordinary sql.Open path intentionally uses the default system library.
	if _, err := Version(); err != nil {
		t.Skipf("no default system library: %v", err)
	}
	db, err := sql.Open(DriverName, ":memory:")
	if err != nil {
		t.Fatal(err)
	}
	defer db.Close()
	var n int
	if err := db.QueryRow("SELECT 1").Scan(&n); err != nil || n != 1 {
		t.Fatal(n, err)
	}
}

func TestFeatureDetection(t *testing.T) {
	c := testConnector(t, ":memory:")
	t.Logf("SQLite %s %s options=%s", c.Version(), c.LibraryPath(), strings.Join(c.CompileOptions(), ","))
	if _, err := c.HasFeature("unknown"); !errors.Is(err, ErrUnsupported) {
		t.Fatal(err)
	}
	for _, name := range []string{"fts3", "fts4", "fts5", "json", "jsonb", "rtree", "math", "dbstat", "geopoly", "extensions"} {
		enabled, err := c.HasFeature(name)
		if err != nil {
			t.Fatalf("%s: %v", name, err)
		}
		t.Log(name, enabled)
	}
}
