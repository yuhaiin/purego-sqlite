package sqlite

import (
	"bytes"
	"database/sql"
	"errors"
	"fmt"
	"runtime"
	"strings"
	"sync"
	"testing"
)

func TestExecDrainsRowsAndPreservesResults(t *testing.T) {
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
