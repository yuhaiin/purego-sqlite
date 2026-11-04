package sqlite

import (
	"bytes"
	"database/sql"
	"errors"
	"fmt"
	"runtime"
	"slices"
	"strings"
	"sync"
	"testing"
)

func TestMetadataCopiesUnicodeAndExpressions(t *testing.T) {
	db := testDB(t)
	db.SetMaxOpenConns(1)
	name := strings.Repeat("列🌍", 128)
	exec(t, db, `CREATE TABLE t("`+name+`" VARCHAR(100))`)
	exec(t, db, "INSERT INTO t VALUES('hello')")
	s, err := db.Prepare(`SELECT "` + name + `", :ключ AS "別名🌍", 1+1 AS "式" FROM t`)
	if err != nil {
		t.Fatal(err)
	}
	defer s.Close()
	r, err := s.Query(sql.Named("ключ", 9))
	if err != nil {
		t.Fatal(err)
	}
	defer r.Close()
	columns, err := r.Columns()
	if err != nil {
		t.Fatal(err)
	}
	types, err := r.ColumnTypes()
	if err != nil {
		t.Fatal(err)
	}
	if !r.Next() {
		t.Fatal("missing row", r.Err())
	}
	var text string
	var named, expression int
	if err := r.Scan(&text, &named, &expression); err != nil || text != "hello" || named != 9 || expression != 2 {
		t.Fatal(text, named, expression, err)
	}
	if err := r.Close(); err != nil {
		t.Fatal(err)
	}
	if err := s.Close(); err != nil {
		t.Fatal(err)
	}
	// Reuse native allocator storage after finalization. Saved Go metadata must
	// remain independent, including NULL decltypes on expressions/parameters.
	for range 30 {
		exec(t, db, `SELECT 1 AS "`+strings.Repeat("替換🌍", 100)+`"`)
	}
	runtime.GC()
	if !slices.Equal(columns, []string{name, "別名🌍", "式"}) {
		t.Fatal(columns)
	}
	if types[0].Name() != name || types[0].DatabaseTypeName() != "VARCHAR" || types[1].DatabaseTypeName() != "" || types[2].DatabaseTypeName() != "" {
		t.Fatal("metadata changed after native finalization")
	}
}

func TestPreparedSQLSurvivesLaterPreparation(t *testing.T) {
	db := testDB(t)
	db.SetMaxOpenConns(1)
	exec(t, db, "CREATE TABLE t(id INTEGER PRIMARY KEY, value TEXT)")
	exec(t, db, "INSERT INTO t VALUES(1,'saved')")
	s, err := db.Prepare("SELECT value AS original FROM t WHERE id=?")
	if err != nil {
		t.Fatal(err)
	}
	defer s.Close()
	for _, query := range []string{"SELECT 42 AS overwritten", `SELECT 42 AS "` + strings.Repeat("large", 2000) + `"`} {
		var got int
		if err := db.QueryRow(query).Scan(&got); err != nil || got != 42 {
			t.Fatal(got, err)
		}
	}
	if _, err := db.Exec("SELECT 1; SELECT 2"); !errors.Is(err, ErrMultipleStatements) {
		t.Fatal(err)
	}
	// Force native automatic reprepare from SQLite's retained SQL copy after
	// several later preparations, including large and rejected SQL inputs.
	exec(t, db, "ALTER TABLE t ADD COLUMN other TEXT")
	r, err := s.Query(1)
	if err != nil {
		t.Fatal(err)
	}
	defer r.Close()
	columns, err := r.Columns()
	if err != nil || !slices.Equal(columns, []string{"original"}) {
		t.Fatal(columns, err)
	}
	if !r.Next() {
		t.Fatal("missing row", r.Err())
	}
	var value string
	if err := r.Scan(&value); err != nil || value != "saved" {
		t.Fatal(value, err)
	}
}

func TestExecDrainsRowsAndPreservesResults(t *testing.T) {
	if testConnector(t, ":memory:").VersionNumber() < 3035000 {
		t.Skip("RETURNING requires SQLite 3.35 or newer")
	}
	db := testDB(t)
	db.SetMaxOpenConns(1)
	exec(t, db, "CREATE TABLE t(id INTEGER PRIMARY KEY, value INTEGER UNIQUE)")
	s, err := db.Prepare("INSERT INTO t(value) VALUES(?),(?) RETURNING id")
	if err != nil {
		t.Fatal(err)
	}
	defer s.Close()
	check := func(r sql.Result, wantID, wantChanges int64) {
		t.Helper()
		id, err := r.LastInsertId()
		if err != nil || id != wantID {
			t.Fatal(id, wantID, err)
		}
		n, err := r.RowsAffected()
		if err != nil || n != wantChanges {
			t.Fatal(n, wantChanges, err)
		}
	}
	r, err := s.Exec(10, 20)
	if err != nil {
		t.Fatal(err)
	}
	check(r, 2, 2)
	check(exec(t, db, "SELECT * FROM t"), 2, 0)
	check(exec(t, db, "UPDATE t SET value=value+1 RETURNING id"), 2, 2)
	// The error must be read before reset, and both reset and clear must run.
	if _, err := s.Exec(30, 11); !errors.Is(err, ErrConstraint) {
		t.Fatal(err)
	}
	r, err = s.Exec(30, 40)
	if err != nil {
		t.Fatal(err)
	}
	check(r, 4, 2)
	// Earlier Result objects must not read the connection's later metadata.
	check(exec(t, db, "DELETE FROM t WHERE id=1"), 4, 1)
	check(r, 4, 2)

	late, err := db.Prepare(`WITH x(v) AS (VALUES(1),(2),(3))
		SELECT CASE WHEN v=? THEN abs(-9223372036854775808) ELSE v END FROM x`)
	if err != nil {
		t.Fatal(err)
	}
	defer late.Close()
	if _, err := late.Exec(3); err == nil || !strings.Contains(err.Error(), "integer overflow") {
		t.Fatal("Exec did not propagate the error after two rows", err)
	}
	r, err = late.Exec(0)
	if err != nil {
		t.Fatal(err)
	}
	check(r, 4, 0)
}

func TestWriteResultsAfterSchemaReprepare(t *testing.T) {
	for _, query := range []string{
		"INSERT INTO t(id) VALUES(?)",
		"\n insert into t(id) values(?)",
		"/* prefix */ INSERT INTO t(id) VALUES(?)",
		"WITH x(id) AS (SELECT ?) INSERT INTO t SELECT id FROM x",
		"REPLACE INTO t(id) VALUES(?)",
	} {
		t.Run(query, func(t *testing.T) {
			db := testDB(t)
			db.SetMaxOpenConns(1)
			exec(t, db, "CREATE TABLE t(id INTEGER PRIMARY KEY)")
			s, err := db.Prepare(query)
			if err != nil {
				t.Fatal(err)
			}
			defer s.Close()
			for id := range 3 {
				if id == 1 {
					exec(t, db, "CREATE TABLE audit(id)")
					exec(t, db, "CREATE TRIGGER audit_insert AFTER INSERT ON t BEGIN INSERT INTO audit VALUES(new.id); END")
				}
				r, err := s.Exec(id + 1)
				if err != nil {
					t.Fatal(err)
				}
				n, err := r.RowsAffected()
				lastID, idErr := r.LastInsertId()
				if err != nil || idErr != nil || n != 1 || lastID != int64(id+1) {
					t.Fatal(n, lastID, err, idErr)
				}
			}
			for _, readOnly := range []string{"SELECT * FROM t", "PRAGMA user_version"} {
				r := exec(t, db, readOnly)
				if n, err := r.RowsAffected(); err != nil || n != 0 {
					t.Fatal(readOnly, n, err)
				}
			}
			r := exec(t, db, "UPDATE t SET id=id WHERE id=99")
			if n, err := r.RowsAffected(); err != nil || n != 0 {
				t.Fatal("no-op DML", n, err)
			}
		})
	}
}

func TestPreparedParameterModeChanges(t *testing.T) {
	db := testDB(t)
	s, err := db.Prepare("SELECT :x, @x, $y")
	if err != nil {
		t.Fatal(err)
	}
	defer s.Close()
	check := func(want [3]int, args ...any) {
		t.Helper()
		var got [3]int
		if err := s.QueryRow(args...).Scan(&got[0], &got[1], &got[2]); err != nil || got != want {
			t.Fatal(got, want, err)
		}
	}
	check([3]int{1, 2, 3}, 1, 2, 3)
	check([3]int{7, 7, 9}, sql.Named("x", 7), sql.Named("y", 9))
	var a, b, c int
	if err := s.QueryRow(1, 2).Scan(&a, &b, &c); err == nil {
		t.Fatal("missing positional argument reused an earlier binding")
	}
	if err := s.QueryRow(sql.Named("x", 1), sql.Named("x", 2), sql.Named("y", 3)).Scan(&a, &b, &c); err == nil {
		t.Fatal("duplicate named argument accepted")
	}
	check([3]int{4, 5, 6}, 4, 5, 6)
	check([3]int{8, 8, 10}, sql.Named("y", 10), sql.Named("x", 8))
}

func TestPreparedExecRebindsAllSlots(t *testing.T) {
	db := testDB(t)
	db.SetMaxOpenConns(1)
	exec(t, db, "CREATE TABLE t(id INTEGER PRIMARY KEY, value)")
	s, err := db.Prepare("INSERT INTO t VALUES(?,?)")
	if err != nil {
		t.Fatal(err)
	}
	defer s.Close()
	for _, args := range [][]any{{1, 10}, {2, 20}, {3, "你好\x00🌍"}, {4, []byte{0, 255}}, {5, nil}} {
		if _, err := s.Exec(args...); err != nil {
			t.Fatal(err)
		}
	}
	if _, err := s.Exec(6); err == nil || !strings.Contains(err.Error(), "missing parameter") {
		t.Fatal("partial rebind reached Step", err)
	}
	if _, err := s.Exec(2, 99); !errors.Is(err, ErrConstraint) {
		t.Fatal(err)
	}
	if _, err := s.Exec(6, 60); err != nil {
		t.Fatal(err)
	}
	var count, value int
	if err := db.QueryRow("SELECT count(*) FROM t").Scan(&count); err != nil || count != 6 {
		t.Fatal(count, err)
	}
	if err := db.QueryRow("SELECT value FROM t WHERE id=6").Scan(&value); err != nil || value != 60 {
		t.Fatal(value, err)
	}
	var text string
	var blob []byte
	if err := db.QueryRow("SELECT value FROM t WHERE id=3").Scan(&text); err != nil || text != "你好\x00🌍" {
		t.Fatal(text, err)
	}
	if err := db.QueryRow("SELECT value FROM t WHERE id=4").Scan(&blob); err != nil || !bytes.Equal(blob, []byte{0, 255}) {
		t.Fatal(blob, err)
	}
}

func TestConnectionFramesAndBufferOwnership(t *testing.T) {
	db := testDB(t)
	db.SetMaxOpenConns(4)
	var wg sync.WaitGroup
	start := make(chan struct{})
	for worker := range 4 {
		c, err := db.Conn(t.Context())
		if err != nil {
			t.Fatal(err)
		}
		wg.Go(func() {
			defer c.Close()
			<-start
			s, err := c.PrepareContext(t.Context(), "SELECT ?1,?2,?3,?4 UNION ALL SELECT 0,0,'',x''")
			if err != nil {
				t.Error(err)
				return
			}
			defer s.Close()
			for i := range 30 {
				n := int64(worker*1000 + i)
				text := fmt.Sprintf("worker-%d-你好\x00🌍", n)
				blob := bytes.Repeat([]byte{byte(worker), byte(i), 255, 0}, 257)
				r, err := s.QueryContext(t.Context(), n, float64(n)+0.25, text, blob)
				if err != nil {
					t.Error(err)
					return
				}
				var gotN int64
				var gotFloat float64
				var gotText string
				var gotBlob []byte
				if !r.Next() {
					t.Error("missing first row", r.Err())
					r.Close()
					return
				}
				if err := r.Scan(&gotN, &gotFloat, &gotText, &gotBlob); err != nil {
					t.Error(err)
				}
				// Invalidate native column storage with another step and reset.
				if !r.Next() {
					t.Error("missing second row", r.Err())
				}
				if err := r.Close(); err != nil {
					t.Error(err)
				}
				if i%10 == 0 {
					runtime.GC()
				}
				if gotN != n || gotFloat != float64(n)+0.25 || gotText != text || !bytes.Equal(gotBlob, blob) {
					t.Error("cross-connection argument corruption or borrowed buffer escaped")
					return
				}
			}
		})
	}
	close(start)
	wg.Wait()
}
