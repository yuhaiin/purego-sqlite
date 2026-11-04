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
	"reflect"
	"runtime"
	"testing"
	"unsafe"

	"github.com/yuhaiin/purego-sqlite/internal/ffi"
)

func TestRowReaderSelection(t *testing.T) {
	c := testConnector(t, ":memory:")
	got, err := c.HasFeature("row_read_v1")
	if err != nil {
		t.Fatal(err)
	}
	if expected := os.Getenv("SQLITE_TEST_ROW_BRIDGE"); expected != "" && got != (expected == "1") {
		t.Fatalf("row reader available=%v, expected %s", got, expected)
	}
	if unsafe.Sizeof(ffi.RowValue{}) != 24 || unsafe.Offsetof(ffi.RowValue{}.Data) != 8 ||
		unsafe.Offsetof(ffi.RowValue{}.Size) != 16 || unsafe.Offsetof(ffi.RowValue{}.Type) != 20 {
		t.Fatal("row reader ABI layout changed")
	}
}

func TestRowReaderValuesAndLifetime(t *testing.T) {
	for _, fast := range []bool{false, true} {
		name := "standard"
		if fast {
			name = "bridge"
		}
		t.Run(name, func(t *testing.T) {
			dc, err := testConnector(t, ":memory:").Connect(t.Context())
			if err != nil {
				t.Fatal(err)
			}
			defer dc.Close()
			c := dc.(*Conn)
			// Change only this connection, never the cached library API.
			api := *c.api
			c.api = &api
			calls := 0
			if fast {
				if api.ReadRow == nil {
					t.Skip("library has no optional row reader")
				}
				read := api.ReadRow
				api.ReadRow = func(stmt uintptr, values []ffi.RowValue) int32 {
					calls++
					return read(stmt, values)
				}
			} else {
				api.ReadRow = nil
			}
			if _, err := c.ExecContext(t.Context(), "PRAGMA encoding='UTF-16le'", nil); err != nil {
				t.Fatal(err)
			}
			if _, err := c.ExecContext(t.Context(), "CREATE TABLE t(a,b,c,d,e)", nil); err != nil {
				t.Fatal(err)
			}
			want := [][]driver.Value{
				{int64(math.MaxInt64), math.Pi, "你好\x00🌍", bytes.Repeat([]byte{0, 255, 128}, 40000), nil},
				{int64(math.MinInt64), math.Inf(-1), "", []byte{}, nil},
				{"type changed", []byte{1, 2}, int64(-7), 1.25, nil},
				{nil, nil, nil, nil, nil},
			}
			for _, row := range want {
				args := make([]driver.NamedValue, len(row))
				for i, value := range row {
					args[i] = driver.NamedValue{Ordinal: i + 1, Value: value}
				}
				if _, err := c.ExecContext(t.Context(), "INSERT INTO t VALUES(?,?,?,?,?)", args); err != nil {
					t.Fatal(err)
				}
			}
			r, err := c.QueryContext(t.Context(), "SELECT * FROM t ORDER BY rowid", nil)
			if err != nil {
				t.Fatal(err)
			}
			if fast {
				// Invalid callers must fail before writing outside the output buffer.
				if rc := api.ReadRow(r.(*rows).s.ptr, nil); rc != 21 {
					t.Fatalf("wrong column count returned %d, want SQLITE_MISUSE", rc)
				}
				calls = 0
			}
			var got [][]driver.Value
			for range want {
				values := make([]driver.Value, 5)
				if err := r.Next(values); err != nil {
					t.Fatal(err)
				}
				got = append(got, values)
			}
			if err := r.Next(make([]driver.Value, 5)); !errors.Is(err, io.EOF) {
				t.Fatal(err)
			}
			if err := r.Close(); err != nil {
				t.Fatal(err)
			}
			if fast && calls != len(want) {
				t.Fatalf("row reader called %d times, want %d", calls, len(want))
			}
			// Invalidate SQLite's buffers before inspecting previously returned rows.
			if _, err := c.ExecContext(t.Context(), "DELETE FROM t", nil); err != nil {
				t.Fatal(err)
			}
			runtime.GC()
			if !reflect.DeepEqual(got, want) {
				t.Fatal("values changed after step/finalize or storage classes were lost")
			}
			floats := []float64{math.Copysign(0, -1), math.SmallestNonzeroFloat64, math.MaxFloat64}
			args := make([]driver.NamedValue, len(floats))
			for i, value := range floats {
				args[i] = driver.NamedValue{Ordinal: i + 1, Value: value}
			}
			r, err = c.QueryContext(t.Context(), "SELECT ?,?,?", args)
			if err != nil {
				t.Fatal(err)
			}
			values := make([]driver.Value, len(floats))
			if err := r.Next(values); err != nil {
				t.Fatal(err)
			}
			for i, value := range floats {
				if math.Float64bits(values[i].(float64)) != math.Float64bits(value) {
					t.Fatalf("floating point bits changed for %g", value)
				}
			}
			if err := r.Close(); err != nil {
				t.Fatal(err)
			}
		})
	}
}

func TestRowReaderErrorDoesNotRetry(t *testing.T) {
	dc, err := testConnector(t, ":memory:").Connect(t.Context())
	if err != nil {
		t.Fatal(err)
	}
	defer dc.Close()
	c := dc.(*Conn)
	api := *c.api
	c.api = &api
	calls, fallback := 0, 0
	api.ReadRow = func(uintptr, []ffi.RowValue) int32 { calls++; return 7 }
	columnType := api.ColumnType
	api.ColumnType = func(stmt uintptr, index int32) int32 { fallback++; return columnType(stmt, index) }
	r, err := c.QueryContext(t.Context(), "SELECT 1", nil)
	if err != nil {
		t.Fatal(err)
	}
	err = r.Next(make([]driver.Value, 1))
	if e, ok := errors.AsType[*Error](err); !ok || e.Code != 7 || calls != 1 || fallback != 0 {
		t.Fatalf("read error=%v calls=%d fallback=%d", err, calls, fallback)
	}
	if !r.(*rows).closed {
		t.Fatal("failed read retained the active statement")
	}
	api.ReadRow = nil
	if _, err := c.ExecContext(t.Context(), "SELECT 2", nil); err != nil {
		t.Fatal("connection unusable after failed read", err)
	}
}

func TestRowReaderScratchOwnership(t *testing.T) {
	dc, err := testConnector(t, ":memory:").Connect(t.Context())
	if err != nil {
		t.Fatal(err)
	}
	defer dc.Close()
	c := dc.(*Conn)
	api := *c.api
	c.api = &api
	if api.ReadRow == nil {
		t.Skip("library has no optional row reader")
	}
	alloc, free := api.Malloc, api.Free
	allocations, frees := 0, 0
	api.Malloc = func(n int32) *byte { allocations++; return alloc(n) }
	api.Free = func(p *byte) { frees++; free(p) }
	for _, query := range []string{"SELECT 1", "SELECT 1,2,3", "SELECT 4,5", "SELECT 6,7,8,9", "SELECT 10"} {
		r, err := c.QueryContext(t.Context(), query, nil)
		if err != nil {
			t.Fatal(err)
		}
		if err := r.Next(make([]driver.Value, len(r.Columns()))); err != nil {
			t.Fatal(err)
		}
		if err := r.Close(); err != nil {
			t.Fatal(err)
		}
	}
	if allocations != 3 || frees != 2 {
		t.Fatalf("scratch was not reused/grown: allocations=%d frees=%d", allocations, frees)
	}
	if err := c.Close(); err != nil {
		t.Fatal(err)
	}
	if frees != allocations || c.rowStorage != nil || c.rowValues != nil {
		t.Fatal("connection close leaked native row scratch")
	}
}

func TestRowReaderAllocationFailure(t *testing.T) {
	dc, err := testConnector(t, ":memory:").Connect(t.Context())
	if err != nil {
		t.Fatal(err)
	}
	defer dc.Close()
	c := dc.(*Conn)
	api := *c.api
	c.api = &api
	api.Malloc = func(int32) *byte { return nil }
	api.ReadRow = func(uintptr, []ffi.RowValue) int32 { t.Fatal("read called after allocation failure"); return 0 }
	r, err := c.QueryContext(t.Context(), "SELECT 1", nil)
	if err != nil {
		t.Fatal(err)
	}
	err = r.Next(make([]driver.Value, 1))
	if e, ok := errors.AsType[*Error](err); !ok || e.Code != 7 || !r.(*rows).closed || c.rowStorage != nil {
		t.Fatalf("allocation failure did not clean up: %v", err)
	}
}

// BenchmarkRowReader changes only ReadRow on each connection: both cases use
// the same native library, compiler flags, database/sql loop and row contents.
func BenchmarkRowReader(b *testing.B) {
	for _, fast := range []bool{false, true} {
		name := "standard"
		if fast {
			name = "bridge"
		}
		b.Run(name, func(b *testing.B) {
			c := testConnector(b, filepath.Join(b.TempDir(), "scan.db"), WithWAL(), WithInit(func(_ context.Context, c *Conn) error {
				if !fast {
					api := *c.api
					api.ReadRow = nil
					c.api = &api
				}
				return nil
			}))
			if fast && c.api.ReadRow == nil {
				b.Skip("library has no optional row reader")
			}
			db := sql.OpenDB(c)
			defer db.Close()
			db.SetMaxOpenConns(1)
			exec(b, db, "PRAGMA synchronous=NORMAL")
			exec(b, db, "PRAGMA cache_size=-2000")
			exec(b, db, "CREATE TABLE items(id INTEGER PRIMARY KEY,name TEXT,value INTEGER)")
			exec(b, db, "WITH RECURSIVE seq(i) AS (VALUES(0) UNION ALL SELECT i+1 FROM seq WHERE i<999) INSERT INTO items SELECT i,printf('name-%04d',i),i FROM seq")
			b.ReportAllocs()
			for b.Loop() {
				r, err := db.Query("SELECT id,name,value FROM items")
				if err != nil {
					b.Fatal(err)
				}
				count := 0
				for r.Next() {
					var id, value int
					var name string
					if err := r.Scan(&id, &name, &value); err != nil {
						b.Fatal(err)
					}
					count++
				}
				if err := r.Err(); err != nil {
					b.Fatal(err)
				}
				r.Close()
				if count != 1000 {
					b.Fatal(count)
				}
			}
			b.ReportMetric(1000, "rows/op")
		})
	}
}
