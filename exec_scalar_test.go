package sqlite

import (
	"context"
	"database/sql"
	"database/sql/driver"
	"errors"
	"fmt"
	"io"
	"math"
	"os"
	"path/filepath"
	"reflect"
	"strings"
	"testing"
)

func TestScalarExecutorSelection(t *testing.T) {
	got, err := testConnector(t, ":memory:").HasFeature("exec_scalar_v1")
	if err != nil {
		t.Fatal(err)
	}
	if expected := os.Getenv("SQLITE_TEST_EXEC_BRIDGE"); expected != "" && got != (expected == "1") {
		t.Fatalf("scalar executor available=%v, expected %s", got, expected)
	}
}

func scalarTestConn(t *testing.T) *Conn {
	t.Helper()
	dc, err := testConnector(t, ":memory:").Connect(t.Context())
	if err != nil {
		t.Fatal(err)
	}
	c := dc.(*Conn)
	t.Cleanup(func() {
		if err := c.Close(); err != nil {
			t.Error(err)
		}
	})
	api := *c.api
	c.api = &api
	if api.ExecScalar == nil {
		t.Skip("library has no optional scalar executor")
	}
	return c
}

func scalarArgs(values ...any) []driver.NamedValue {
	args := make([]driver.NamedValue, len(values))
	for i, value := range values {
		args[i] = driver.NamedValue{Ordinal: i + 1, Value: value}
	}
	return args
}

func TestScalarExecutorValuesAndReuse(t *testing.T) {
	for _, fast := range []bool{false, true} {
		t.Run(fmt.Sprint(fast), func(t *testing.T) {
			c := scalarTestConn(t)
			if !fast {
				c.api.ExecScalar = nil
			}
			if _, err := c.ExecContext(t.Context(), "CREATE TABLE t(i,r,n,b)", nil); err != nil {
				t.Fatal(err)
			}
			s, err := c.PrepareContext(t.Context(), "INSERT INTO t VALUES(?,?,?,?) RETURNING i")
			if err != nil {
				t.Fatal(err)
			}
			defer s.Close()
			want := [][]driver.Value{
				{int64(math.MinInt64), math.SmallestNonzeroFloat64, nil, int64(1)},
				{int64(math.MaxInt64), math.MaxFloat64, nil, int64(0)},
				{int64(0), math.Inf(-1), nil, int64(1)},
				{int64(-1), math.Pi, nil, int64(0)},
			}
			for i, row := range want {
				args := scalarArgs(row[0], row[1], row[2], i%2 == 0)
				r, err := s.(*stmt).ExecContext(t.Context(), args)
				if err != nil {
					t.Fatal(err)
				}
				id, _ := r.LastInsertId()
				changes, _ := r.RowsAffected()
				if id != int64(i+1) || changes != 1 {
					t.Fatalf("incorrect result: id=%d changes=%d", id, changes)
				}
			}
			r, err := c.QueryContext(t.Context(), "SELECT * FROM t ORDER BY rowid", nil)
			if err != nil {
				t.Fatal(err)
			}
			for _, row := range want {
				got := make([]driver.Value, 4)
				if err := r.Next(got); err != nil || !reflect.DeepEqual(got, row) {
					t.Fatalf("row=%v want=%v error=%v", got, row, err)
				}
			}
			if err := r.Next(make([]driver.Value, 4)); !errors.Is(err, io.EOF) {
				t.Fatal(err)
			}
			r.Close()
			result, err := c.ExecContext(t.Context(), "SELECT 1 UNION ALL SELECT 2", nil)
			if err != nil {
				t.Fatal(err)
			}
			if n, _ := result.RowsAffected(); n != 0 {
				t.Fatal("read-only Exec inherited previous changes", n)
			}
		})
	}
}

func TestScalarExecutorErrorsAndFallback(t *testing.T) {
	c := scalarTestConn(t)
	if _, err := c.ExecContext(t.Context(), "CREATE TABLE t(v UNIQUE)", nil); err != nil {
		t.Fatal(err)
	}
	prepared, err := c.PrepareContext(t.Context(), "INSERT INTO t VALUES(?)")
	if err != nil {
		t.Fatal(err)
	}
	s := prepared.(*stmt)
	defer s.Close()
	call, calls := c.api.ExecScalar, 0
	c.api.ExecScalar = func(stmt uintptr, frame []uint64) int32 {
		calls++
		return call(stmt, frame)
	}
	for _, value := range []any{int64(1), "text", []byte{1, 0}, nil, float64(2.5), true} {
		_, err := s.ExecContext(t.Context(), scalarArgs(value))
		if value == true {
			if e, ok := errors.AsType[*Error](err); !ok || e.ExtendedCode != 2067 || !strings.Contains(e.Message, "t.v") {
				t.Fatal("detailed constraint error lost", err)
			}
		} else if err != nil {
			t.Fatal(err)
		}
	}
	if calls != 4 {
		t.Fatal("payload binding used scalar executor", calls)
	}
	if _, err := s.ExecContext(t.Context(), scalarArgs(int64(3))); err != nil {
		t.Fatal("statement not reusable after constraint", err)
	}
	named := []driver.NamedValue{{Name: "v", Ordinal: 1, Value: int64(4)}}
	if _, err := c.ExecContext(t.Context(), "INSERT INTO t VALUES(:v)", named); err != nil {
		t.Fatal(err)
	}
	if calls != 5 {
		t.Fatal("named binding used scalar executor", calls)
	}
	if _, err := s.ExecContext(t.Context(), scalarArgs(struct{}{})); err == nil || calls != 5 {
		t.Fatal("invalid type was executed", err, calls)
	}
	if _, err := s.ExecContext(t.Context(), nil); err == nil || calls != 5 {
		t.Fatal("missing parameter was executed", err, calls)
	}
	// A helper failure must never fall through to the ordinary executor.
	c.api.ExecScalar = func(uintptr, []uint64) int32 { return 7 }
	c.api.Exec = func(uintptr, uintptr, bool) (int32, int64, int64) {
		t.Fatal("failed bridge retried execution")
		return 0, 0, 0
	}
	if _, err := s.ExecContext(t.Context(), scalarArgs(int64(5))); err == nil {
		t.Fatal("helper error discarded")
	}
	c.api.ExecScalar = call
	if _, err := s.ExecContext(t.Context(), scalarArgs(int64(5))); err != nil {
		t.Fatal("failed helper left statement unusable", err)
	}
}

func TestScalarExecutorScratchAndNativeValidation(t *testing.T) {
	c := scalarTestConn(t)
	alloc, free := c.api.Malloc, c.api.Free
	allocations, frees := 0, 0
	c.api.Malloc = func(n int32) *byte { allocations++; return alloc(n) }
	c.api.Free = func(p *byte) { frees++; free(p) }
	for _, query := range []string{"SELECT ?", "SELECT ?", "SELECT ?,?,?", "SELECT ?"} {
		args := scalarArgs(int64(1))
		if strings.Count(query, "?") == 3 {
			args = scalarArgs(int64(1), float64(2.5), nil)
		}
		if _, err := c.ExecContext(t.Context(), query, args); err != nil {
			t.Fatal(err)
		}
	}
	if allocations != 2 || frees != 1 {
		t.Fatalf("scratch not reused/grown: allocations=%d frees=%d", allocations, frees)
	}
	s, err := c.PrepareContext(t.Context(), "SELECT ?")
	if err != nil {
		t.Fatal(err)
	}
	defer s.Close()
	if rc := c.api.ExecScalar(s.(*stmt).ptr, c.execFrame[:2]); rc != 21 {
		t.Fatal("wrong count did not return MISUSE", rc)
	}
	c.execFrame[3] = 3 // TEXT is invalid for this scalar-only ABI.
	if rc := c.api.ExecScalar(s.(*stmt).ptr, c.execFrame[:4]); rc != 20 {
		t.Fatal("wrong type did not return MISMATCH", rc)
	}
	// A failed grow must retain the existing allocation without executing SQL.
	c.api.Malloc = func(int32) *byte { return nil }
	_, err = c.ExecContext(t.Context(), "SELECT ?,?,?,?", scalarArgs(int64(1), nil, nil, nil))
	if e, ok := errors.AsType[*Error](err); !ok || e.Code != 7 || frees != 1 || len(c.execFrame) != 8 {
		t.Fatal("failed grow lost buffer or NOMEM", err)
	}
	if err := c.Close(); err != nil {
		t.Fatal(err)
	}
	if frees != allocations || c.execFrame != nil || c.execStorage != nil {
		t.Fatal("scalar scratch leaked")
	}
}

// BenchmarkScalarExec compares both paths using exactly the same loaded library.
func BenchmarkScalarExec(b *testing.B) {
	for _, workload := range []struct {
		name   string
		rows   int
		values bool
	}{{"BatchInsert1000", 1000, false}, {"BatchInsert1000Values", 1000, true}, {"TransactionInsert100000", 100000, false}} {
		b.Run(workload.name, func(b *testing.B) {
			for _, fast := range []bool{false, true} {
				name := "standard"
				if fast {
					name = "bridge"
				}
				b.Run(name, func(b *testing.B) {
					connector := testConnector(b, filepath.Join(b.TempDir(), "writes.db"), WithWAL(), WithInit(func(_ context.Context, c *Conn) error {
						api := *c.api
						if !fast {
							api.ExecScalar = nil
						}
						c.api = &api
						return nil
					}))
					if fast && connector.api.ExecScalar == nil {
						b.Skip("library has no scalar executor")
					}
					db := sql.OpenDB(connector)
					defer db.Close()
					db.SetMaxOpenConns(1)
					exec(b, db, "PRAGMA synchronous=NORMAL")
					exec(b, db, "PRAGMA cache_size=-2000")
					exec(b, db, "PRAGMA busy_timeout=5000")
					exec(b, db, "CREATE TABLE events(id INTEGER PRIMARY KEY, value INTEGER)")
					query := "INSERT INTO events(value) VALUES(?)"
					args := []any{int64(0)}
					if workload.values {
						query = "INSERT INTO events(value) VALUES" + strings.TrimSuffix(strings.Repeat("(?),", workload.rows), ",")
						args = make([]any, workload.rows)
						for i := range args {
							args[i] = int64(i)
						}
					}
					b.ReportAllocs()
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
						n := workload.rows
						if workload.values {
							n = 1
						}
						for i := range n {
							if !workload.values {
								args[0] = i
							}
							if _, err := s.Exec(args...); err != nil {
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
					b.ReportMetric(float64(workload.rows), "rows/op")
				})
			}
		})
	}
}
