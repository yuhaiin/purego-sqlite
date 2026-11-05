package sqlite

import (
	"bytes"
	"context"
	"database/sql"
	"database/sql/driver"
	"errors"
	"fmt"
	"math"
	"strings"
	"testing"

	"github.com/yuhaiin/purego-sqlite/internal/ffi"
)

type sumAggregate struct{ sum int64 }

func (a *sumAggregate) Step(value int64) { a.sum += value }
func (a *sumAggregate) Done() int64      { return a.sum }

type sumInterface interface {
	Step(int64)
	Done() int64
}

type failingAggregate struct{ phase string }

func (a *failingAggregate) Step(int64) error {
	if a.phase == "panic" {
		panic("aggregate panic")
	}
	if a.phase == "step" {
		return errors.New("step failure")
	}
	return nil
}
func (a *failingAggregate) Done() (int64, error) { return 0, errors.New("done failure") }

func TestAggregateFailuresSecurityAndCancellation(t *testing.T) {
	ctx, cancel := context.WithCancel(t.Context())
	defer cancel()
	db := testDB(t, WithInit(func(_ context.Context, c *Conn) error {
		for _, phase := range []string{"step", "done", "panic"} {
			if err := c.RegisterAggregator(phase+"_failure", func() *failingAggregate { return &failingAggregate{phase: phase} }, true); err != nil {
				return err
			}
		}
		if err := c.RegisterAggregator("constructor_failure", func() (*sumAggregate, error) { return nil, errors.New("constructor failure") }, true); err != nil {
			return err
		}
		if err := c.RegisterAggregator("cancel_sum", func() *sumAggregate { return &sumAggregate{} }, true); err != nil {
			return err
		}
		return c.RegisterFunc("cancel_here", func(value int64) int64 {
			if value == 2 {
				cancel()
			}
			return value
		}, false)
	}))
	for _, query := range []string{"SELECT step_failure(1)", "SELECT done_failure(1)", "SELECT panic_failure(1)", "SELECT constructor_failure(1)"} {
		var result int64
		if err := db.QueryRow(query).Scan(&result); err == nil {
			t.Fatalf("accepted %s", query)
		}
	}
	var total int64
	if err := db.QueryRowContext(ctx, "WITH vals(v) AS (VALUES(1),(2),(3)) SELECT cancel_sum(cancel_here(v)) FROM vals").Scan(&total); !errors.Is(err, context.Canceled) {
		t.Fatalf("callback cancellation: %v", err)
	}
	if err := db.QueryRow("SELECT cancel_sum(9)").Scan(&total); err != nil || total != 9 {
		t.Fatal(total, err)
	}
	connector := testConnector(t, ":memory:")
	if connector.VersionNumber() < 3031000 {
		return
	}
	dc, err := connector.Connect(t.Context())
	if err != nil {
		t.Fatal(err)
	}
	c := dc.(*Conn)
	defer c.Close()
	if err := c.RegisterFunc("safe_fn", func() int64 { return 1 }, true, FunctionInnocuous); err != nil {
		t.Fatal(err)
	}
	if _, err := c.ExecContext(t.Context(), "PRAGMA trusted_schema=OFF; CREATE TABLE safe(v GENERATED ALWAYS AS(safe_fn()), x)", nil); err != nil {
		t.Fatal(err)
	}
	if err := c.RegisterFunc("direct_fn", func() int64 { return 1 }, true, FunctionDirectOnly); err != nil {
		t.Fatal(err)
	}
	if _, err := c.ExecContext(t.Context(), "CREATE TABLE unsafe(v GENERATED ALWAYS AS(direct_fn()), x)", nil); err == nil {
		t.Fatal("DIRECTONLY allowed schema use")
	}
}

func TestFunctionsValuesErrorsAndPanic(t *testing.T) {
	before := ffi.CallbackCount()
	db := testDB(t, WithInit(func(_ context.Context, c *Conn) error {
		functions := []struct {
			name string
			fn   any
		}{
			{"add_values", func(a int64, rest ...int64) int64 {
				for _, value := range rest {
					a += value
				}
				return a
			}},
			{"text_echo", func(value string) string { return value }},
			{"blob_echo", func(value []byte) []byte { return value }},
			{"float_echo", func(value float64) float64 { return value }},
			{"bool_echo", func(value bool) bool { return value }},
			{"null_result", func() any { return nil }},
			{"empty_blob", func() []byte { return []byte{} }},
			{"generic_echo", func(value any) any { return value }},
			{"fail_fn", func() (int64, error) { return 0, errors.New("function failure") }},
			{"panic_fn", func() string { panic("function panic") }},
			{"reentry", func() (int64, error) {
				_, err := c.ExecContext(context.Background(), "SELECT 1", nil)
				if !errors.Is(err, ErrCallbackReentry) {
					return 0, fmt.Errorf("reentry not rejected: %v", err)
				}
				return 1, nil
			}},
		}
		for _, function := range functions {
			if err := c.RegisterFunc(function.name, function.fn, true); err != nil {
				return err
			}
		}
		return nil
	}))
	defer func() {
		db.Close()
		if after := ffi.CallbackCount(); after != before {
			t.Errorf("leaked callbacks: %d -> %d", before, after)
		}
	}()
	var integer int64
	if err := db.QueryRow("SELECT add_values(1,2,3)").Scan(&integer); err != nil || integer != 6 {
		t.Fatalf("variadic: %d %v", integer, err)
	}
	for _, payload := range []string{"", "x\x00y世界"} {
		var got string
		if err := db.QueryRow("SELECT text_echo(?)", payload).Scan(&got); err != nil || got != payload {
			t.Fatalf("text %q %v", got, err)
		}
	}
	for _, payload := range [][]byte{{}, {0, 1, 255}} {
		var got []byte
		if err := db.QueryRow("SELECT blob_echo(?)", payload).Scan(&got); err != nil || got == nil || !bytes.Equal(got, payload) {
			t.Fatalf("blob %v %v", got, err)
		}
	}
	var number float64
	if err := db.QueryRow("SELECT float_echo(?)", math.Pi).Scan(&number); err != nil || number != math.Pi {
		t.Fatal(number, err)
	}
	if err := db.QueryRow("SELECT bool_echo(-1)").Scan(&integer); err != nil || integer != 1 {
		t.Fatal(integer, err)
	}
	var value any
	if err := db.QueryRow("SELECT null_result()").Scan(&value); err != nil || value != nil {
		t.Fatal(value, err)
	}
	var blob []byte
	if err := db.QueryRow("SELECT empty_blob()").Scan(&blob); err != nil || blob == nil || len(blob) != 0 {
		t.Fatal(blob, err)
	}
	if err := db.QueryRow("SELECT generic_echo(NULL)").Scan(&value); err != nil || value != nil {
		t.Fatal(value, err)
	}
	for _, query := range []string{"SELECT fail_fn()", "SELECT panic_fn()", "SELECT add_values()", "SELECT add_values('wrong')", "SELECT text_echo(NULL)"} {
		if err := db.QueryRow(query).Scan(&value); err == nil {
			t.Fatalf("accepted %s", query)
		}
		if err := db.QueryRow("SELECT reentry()").Scan(&integer); err != nil || integer != 1 {
			t.Fatalf("reuse: %v", err)
		}
	}
}

func TestAggregateGroupsEmptyAndInterface(t *testing.T) {
	db := testDB(t, WithInit(func(_ context.Context, c *Conn) error {
		if err := c.RegisterAggregator("go_sum", func() *sumAggregate { return &sumAggregate{} }, true); err != nil {
			return err
		}
		return c.RegisterAggregator("go_iface_sum", func() sumInterface { return &sumAggregate{} }, true)
	}))
	exec(t, db, "CREATE TABLE data(k,v); INSERT INTO data VALUES ('a',2),('b',7),('a',3),('b',11)")
	rows, err := db.Query("SELECT k,go_sum(v),go_iface_sum(v) FROM data GROUP BY k ORDER BY k")
	if err != nil {
		t.Fatal(err)
	}
	defer rows.Close()
	for _, want := range []int64{5, 18} {
		if !rows.Next() {
			t.Fatal(rows.Err())
		}
		var key string
		var a, b int64
		if err := rows.Scan(&key, &a, &b); err != nil || a != want || b != want {
			t.Fatal(key, a, b, err)
		}
	}
	if err := rows.Close(); err != nil {
		t.Fatal(err)
	}
	var sum int64
	if err := db.QueryRow("SELECT go_sum(v) FROM data WHERE 0").Scan(&sum); err != nil || sum != 0 {
		t.Fatal(sum, err)
	}
}

func TestCollationReplacementPanicAndFunctionLifecycle(t *testing.T) {
	before := ffi.CallbackCount()
	connector := testConnector(t, ":memory:")
	dc, err := connector.Connect(t.Context())
	if err != nil {
		t.Fatal(err)
	}
	c := dc.(*Conn)
	defer func() {
		c.Close()
		if ffi.CallbackCount() != before {
			t.Fatal("callback handles leaked")
		}
	}()
	for range 30 {
		if err := c.RegisterFunc("identity", func(v int64) int64 { return v }, true); err != nil {
			t.Fatal(err)
		}
		if err := c.RegisterCollation("reverse", func(a, b string) int { return strings.Compare(b, a) * 1000 }); err != nil {
			t.Fatal(err)
		}
	}
	if ffi.CallbackCount() != before+2 {
		t.Fatal("replacement leaked handles")
	}
	db := sql.OpenDB(&singleConnector{conn: c})
	defer db.Close()
	exec(t, db, "CREATE TABLE values_table(v); INSERT INTO values_table VALUES ('a'),('c'),('b'); CREATE INDEX custom_index ON values_table(identity(1))")
	var got string
	if err := db.QueryRow("SELECT group_concat(v,'') FROM (SELECT v FROM values_table ORDER BY v COLLATE reverse)").Scan(&got); err != nil || got != "cba" {
		t.Fatal(got, err)
	}
	if err := c.RegisterCollation("reverse", nil); err != nil {
		t.Fatal(err)
	}
	if err := c.RegisterCollation("bad", func(a, b string) int { panic("comparison panic") }); err != nil {
		t.Fatal(err)
	}
	if err := db.QueryRow("SELECT v FROM values_table ORDER BY v COLLATE bad").Scan(&got); err == nil || !strings.Contains(err.Error(), "comparison panic") {
		t.Fatalf("collation panic: %v", err)
	}
	if err := db.QueryRow("SELECT 'ok'").Scan(&got); err != nil || got != "ok" {
		t.Fatal(got, err)
	}
	for _, fn := range []any{nil, 17, func() {}, func(v chan int) int { return 0 }, func() chan int { return nil }} {
		if err := c.RegisterFunc("invalid", fn, true); err == nil {
			t.Fatalf("invalid signature %T accepted", fn)
		}
	}
}

func TestCallbackChurnAndMultipleEngines(t *testing.T) {
	before := ffi.CallbackCount()
	connector := testConnector(t, ":memory:")
	iterations := 10000
	if testing.Short() {
		iterations = 100
	}
	for i := range iterations {
		dc, err := connector.Connect(t.Context())
		if err != nil {
			t.Fatal(err)
		}
		c := dc.(*Conn)
		if err := c.RegisterFunc("value", func() int64 { return 1 }, true); err != nil {
			t.Fatal(i, err)
		}
		if err := c.Close(); err != nil {
			t.Fatal(err)
		}
	}
	if ffi.CallbackCount() != before {
		t.Fatal("connection churn leaked registry handles")
	}
	// Compare the selected native library against the default linked/system
	// engine. Windows PureGo may have no implicit system DLL, which is intentional.
	other, err := NewConnector(":memory:")
	if err != nil {
		t.Log("no default engine for multi-library comparison:", err)
		return
	}
	for i, candidate := range []*Connector{connector, other} {
		dc, err := candidate.Connect(t.Context())
		if err != nil {
			t.Fatal(err)
		}
		c := dc.(*Conn)
		if err := c.RegisterFunc("library_value", func() int64 { return int64(i + 1) }, true); err != nil {
			t.Fatal(err)
		}
		rows, err := c.QueryContext(t.Context(), "SELECT library_value()", nil)
		if err != nil {
			t.Fatal(err)
		}
		values := make([]driver.Value, 1)
		if err := rows.Next(values); err != nil || values[0] != int64(i+1) {
			t.Fatal(values, err)
		}
		rows.Close()
		c.Close()
	}
}

func TestCallbackRegistrationFailureCleansTokens(t *testing.T) {
	before := ffi.CallbackCount()
	connector := testConnector(t, ":memory:", WithInit(func(_ context.Context, c *Conn) error {
		if err := c.RegisterFunc("before_failure", func() int64 { return 1 }, true); err != nil {
			return err
		}
		return errors.New("init failure")
	}))
	for range 10 {
		if dc, err := connector.Connect(t.Context()); err == nil {
			dc.Close()
			t.Fatal("accepted failed init")
		}
	}
	if ffi.CallbackCount() != before {
		t.Fatal("init failure leaked callback")
	}
	dc, err := testConnector(t, ":memory:").Connect(t.Context())
	if err != nil {
		t.Fatal(err)
	}
	c := dc.(*Conn)
	defer c.Close()
	// Go limits reflected signatures before SQLite's modern 1000-argument
	// ceiling. Exercise the shared registrar's genuine native failure path.
	if err := c.registerFunction("too_many_arguments", math.MaxInt32, true, nil, func() ffi.CallbackHandlers {
		return ffi.CallbackHandlers{Scalar: func(uintptr, int32, uintptr) {}}
	}, false); err == nil {
		t.Fatal("accepted invalid native arity")
	}
	if ffi.CallbackCount() != before {
		t.Fatal("native registration failure leaked token")
	}
}

type singleConnector struct{ conn *Conn }

func (s *singleConnector) Driver() driver.Driver                        { return &Driver{} }
func (s *singleConnector) Connect(context.Context) (driver.Conn, error) { return s.conn, nil }
