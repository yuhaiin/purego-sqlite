package sqlite

import (
	"bytes"
	"context"
	"database/sql/driver"
	"errors"
	"fmt"
	"io"
	"path/filepath"
	"runtime"
	"slices"
	"strings"
	"sync"
	"syscall"
	"testing"

	"github.com/yuhaiin/purego-sqlite/internal/ffi"
)

func physicalConn(t testing.TB, opts ...Option) *Conn {
	t.Helper()
	connector := testConnector(t, ":memory:", opts...)
	dc, err := connector.Connect(t.Context())
	if err != nil {
		t.Fatal(err)
	}
	c := dc.(*Conn)
	t.Cleanup(func() {
		if err := c.Close(); err != nil {
			t.Error(err)
		}
	})
	return c
}

func nativeArgs(args []any) []driver.NamedValue {
	values := make([]driver.NamedValue, len(args))
	for i, arg := range args {
		values[i] = driver.NamedValue{Ordinal: i + 1, Value: arg}
	}
	return values
}
func connExec(t testing.TB, c *Conn, query string, args ...any) {
	t.Helper()
	if _, err := c.ExecContext(t.Context(), query, nativeArgs(args)); err != nil {
		t.Fatal(query, err)
	}
}
func connRow(t testing.TB, c *Conn, query string, args ...any) []driver.Value {
	t.Helper()
	r, err := c.QueryContext(t.Context(), query, nativeArgs(args))
	if err != nil {
		t.Fatal(query, err)
	}
	defer r.Close()
	values := make([]driver.Value, len(r.Columns()))
	if err := r.Next(values); err != nil {
		t.Fatal(query, err)
	}
	return values
}

func TestSerializeDeserialize(t *testing.T) {
	c := physicalConn(t, WithStatementCache(4))
	if c.api.Serialize == nil || c.api.Deserialize == nil {
		t.Skip("selected library omits serialization")
	}
	connExec(t, c, "CREATE TABLE data(id INTEGER PRIMARY KEY, value BLOB); INSERT INTO data VALUES(1,x'00ff'); ATTACH ':memory:' AS extra; CREATE TABLE extra.names(name); INSERT INTO extra.names VALUES('snapshot')")
	image, err := c.Serialize("")
	if err != nil || len(image) < 100 {
		t.Fatalf("serialize %d: %v", len(image), err)
	}
	extra, err := c.SerializeContext(t.Context(), "extra")
	if err != nil {
		t.Fatal(err)
	}
	before := bytes.Clone(image)
	dest := physicalConn(t, WithStatementCache(2))
	connExec(t, dest, "CREATE TABLE old(x)")
	if err := dest.Deserialize(image, "main"); err != nil {
		t.Fatal(err)
	}
	clear(image) // The native database must own an independent copy.
	if value := connRow(t, dest, "SELECT value FROM data")[0]; !bytes.Equal(value.([]byte), []byte{0, 255}) {
		t.Fatal(value)
	}
	connExec(t, dest, "INSERT INTO data(value) VALUES(zeroblob(100000))")
	if value := connRow(t, dest, "SELECT length(value) FROM data WHERE id=2")[0]; value != int64(100000) {
		t.Fatal(value)
	}
	if err := dest.Deserialize(extra, ""); err != nil {
		t.Fatal(err)
	}
	if value := connRow(t, dest, "SELECT name FROM names")[0]; value != "snapshot" {
		t.Fatal(value)
	}
	if value := connRow(t, c, "SELECT count(*) FROM data")[0]; value != int64(1) {
		t.Fatal(value)
	}
	for _, schema := range []string{"missing", "main\x00bad"} {
		if _, err := c.Serialize(schema); err == nil {
			t.Fatalf("accepted schema %q", schema)
		}
		if err := dest.Deserialize(before, schema); err == nil {
			t.Fatalf("accepted schema %q", schema)
		}
	}
	if err := dest.Deserialize(before, "temp"); err == nil {
		t.Fatal("accepted temp")
	}
	if err := dest.Deserialize(nil, "main"); err == nil {
		t.Fatal("accepted invalid image")
	}
	wal := bytes.Clone(before)
	wal[18], wal[19] = 2, 2
	if err := dest.Deserialize(wal, "main"); err == nil {
		t.Fatal("accepted WAL image")
	}
	stmt, err := dest.Prepare("SELECT * FROM names")
	if err != nil {
		t.Fatal(err)
	}
	if err := dest.Deserialize(before, "main"); err == nil {
		t.Fatal("accepted live statement")
	}
	stmt.Close()
	connExec(t, dest, "BEGIN")
	if err := dest.Deserialize(before, "main"); err == nil {
		t.Fatal("accepted active transaction")
	}
	connExec(t, dest, "ROLLBACK")
	ctx, cancel := context.WithCancel(t.Context())
	cancel()
	if _, err := dest.SerializeContext(ctx, ""); !errors.Is(err, context.Canceled) {
		t.Fatal(err)
	}
	if err := dest.DeserializeContext(ctx, before, ""); !errors.Is(err, context.Canceled) {
		t.Fatal(err)
	}
	for range 100 {
		if err := dest.Deserialize(before, "missing"); err == nil {
			t.Fatal("accepted missing schema")
		}
	}
	if dest.api.SetAuthorizer != nil {
		calls := 0
		if err := dest.RegisterAuthorizer(func(a Authorization) AuthResult {
			if a.Action == 24 {
				calls++
				return AuthDeny
			}
			return AuthAllow
		}); err != nil {
			t.Fatal(err)
		}
		// sqlite3_deserialize prepares ATTACH internally. Native rejection after
		// allocation must free FREEONCLOSE input without a second Go-side free.
		for range 100 {
			if err := dest.Deserialize(before, "main"); !errors.Is(err, ErrAuth) {
				t.Fatal(err)
			}
		}
		if calls != 100 {
			t.Fatal("native failure path not exercised", calls)
		}
		if err := dest.RegisterAuthorizer(nil); err != nil {
			t.Fatal(err)
		}
		if value := connRow(t, dest, "SELECT name FROM names")[0]; value != "snapshot" {
			t.Fatal(value)
		}
	}
}

func TestOptionalMediumAPIs(t *testing.T) {
	c := physicalConn(t)
	api := *c.api
	c.api = &api
	api.Serialize, api.Deserialize, api.SetAuthorizer, api.TraceV2 = nil, nil, nil, nil
	if _, err := c.Serialize(""); !errors.Is(err, ErrUnsupported) {
		t.Fatal(err)
	}
	// Header validation is independent of native availability.
	image := make([]byte, 100)
	copy(image, "SQLite format 3\x00")
	image[18], image[19] = 1, 1
	if err := c.Deserialize(image, ""); !errors.Is(err, ErrUnsupported) {
		t.Fatal(err)
	}
	if err := c.RegisterAuthorizer(nil); !errors.Is(err, ErrUnsupported) {
		t.Fatal(err)
	}
	if err := c.SetTrace(nil); !errors.Is(err, ErrUnsupported) {
		t.Fatal(err)
	}
	if value := connRow(t, c, "SELECT 42")[0]; value != int64(42) {
		t.Fatal(value)
	}
}

func TestBackupFromSchemasProgressAndAbort(t *testing.T) {
	source, dest := physicalConn(t), physicalConn(t, WithStatementCache(4))
	connExec(t, source, "ATTACH ':memory:' AS src; CREATE TABLE src.data(x); INSERT INTO src.data VALUES(zeroblob(200000))")
	connExec(t, dest, "ATTACH ':memory:' AS dst; CREATE TABLE dst.previous(x); INSERT INTO dst.previous VALUES(7)")
	abort := errors.New("stop backup")
	calls := 0
	err := dest.BackupFrom(t.Context(), source, BackupOptions{SourceSchema: "src", DestinationSchema: "dst", PagesPerStep: 1, Progress: func(p BackupProgress) error {
		calls++
		if p.PageCount < 2 || p.Remaining <= 0 || p.Done {
			t.Errorf("progress: %+v", p)
		}
		if _, err := dest.ExecContext(t.Context(), "SELECT 1", nil); !errors.Is(err, ErrCallbackReentry) {
			t.Error(err)
		}
		if _, err := source.Serialize(""); !errors.Is(err, ErrCallbackReentry) {
			t.Error(err)
		}
		return abort
	}})
	if !errors.Is(err, abort) || calls != 1 {
		t.Fatalf("abort: %d %v", calls, err)
	}
	if value := connRow(t, dest, "SELECT x FROM dst.previous")[0]; value != int64(7) {
		t.Fatal(value)
	}
	calls = 0
	finished := false
	err = dest.BackupFrom(t.Context(), source, BackupOptions{SourceSchema: "src", DestinationSchema: "dst", PagesPerStep: 2, Progress: func(p BackupProgress) error {
		calls++
		finished = p.Done
		if p.Done && p.Remaining != 0 {
			t.Error(p)
		}
		return nil
	}})
	if err != nil || calls < 2 || !finished {
		t.Fatalf("copy: %d %v %v", calls, finished, err)
	}
	if value := connRow(t, dest, "SELECT length(x) FROM dst.data")[0]; value != int64(200000) {
		t.Fatal(value)
	}
	connExec(t, dest, "UPDATE dst.data SET x='restored'")
	if err := source.BackupFrom(t.Context(), dest, BackupOptions{SourceSchema: "dst", DestinationSchema: "src"}); err != nil {
		t.Fatal(err)
	}
	if value := connRow(t, source, "SELECT x FROM src.data")[0]; value != "restored" {
		t.Fatal(value)
	}
	if err := dest.BackupFrom(t.Context(), dest, BackupOptions{}); err == nil {
		t.Fatal("accepted same connection")
	}
	if err := dest.BackupFrom(t.Context(), nil, BackupOptions{}); err == nil {
		t.Fatal("accepted nil source")
	}
	ctx, cancel := context.WithCancel(t.Context())
	cancel()
	if err := dest.BackupFrom(ctx, source, BackupOptions{}); !errors.Is(err, context.Canceled) {
		t.Fatal(err)
	}
	if err := dest.BackupFrom(t.Context(), source, BackupOptions{SourceSchema: "missing"}); err == nil {
		t.Fatal("accepted missing schema")
	}
	if err := dest.BackupFrom(t.Context(), source, BackupOptions{SourceSchema: "src", Progress: func(BackupProgress) error { panic("progress") }}); err == nil || !strings.Contains(err.Error(), "progress") {
		t.Fatal(err)
	}
	connExec(t, dest, "BEGIN")
	if err := dest.BackupFrom(t.Context(), source, BackupOptions{}); err == nil {
		t.Fatal("accepted active transaction")
	}
	connExec(t, dest, "ROLLBACK")
}

func TestStatementCacheLifecycle(t *testing.T) {
	c := physicalConn(t, WithStatementCache(2))
	connExec(t, c, "CREATE TABLE items(id INTEGER PRIMARY KEY, value BLOB)")
	connExec(t, c, "INSERT INTO items VALUES(?,?)", int64(1), bytes.Repeat([]byte{1}, 100000))
	if c.cache == nil || c.cache.lru.Len() != 1 {
		t.Fatal("insert not cached")
	}
	for _, entry := range c.cache.entries {
		s := entry.Value.(*stmt)
		if c.api.Step(s.ptr) != 101 {
			t.Fatal("idle statement still had bound values")
		}
		// Cleared INTEGER PRIMARY KEY becomes NULL and allocates a new row.
		// The BLOB must be NULL too, rather than retaining the large payload.
		if err := c.sqliteError(c.api.ResetClear(s.ptr)); err != nil {
			t.Fatal(err)
		}
	}
	if values := connRow(t, c, "SELECT id,value FROM items WHERE id=2"); values[0] != int64(2) || values[1] != nil {
		t.Fatal(values)
	}
	for i := range 20 {
		if value := connRow(t, c, fmt.Sprintf("SELECT %d", i))[0]; value != int64(i) {
			t.Fatal(value)
		}
		if c.cache.lru.Len() > 2 || len(c.stmts) > 2 {
			t.Fatal("unbounded cache")
		}
	}
	r1, err := c.QueryContext(t.Context(), "SELECT id,value FROM items", nil)
	if err != nil {
		t.Fatal(err)
	}
	r1.Close()
	r2, err := c.QueryContext(t.Context(), "SELECT id,value FROM items", nil)
	if err != nil {
		t.Fatal(err)
	}
	if err := r1.Next(make([]driver.Value, 2)); !errors.Is(err, io.EOF) {
		t.Fatal("closed rows reused", err)
	}
	r1.Close()
	if err := r2.Next(make([]driver.Value, 2)); err != nil {
		t.Fatal("old rows closed new rows", err)
	}
	r2.Close()
	connExec(t, c, "ALTER TABLE items ADD COLUMN name TEXT DEFAULT 'updated'")
	r, err := c.QueryContext(t.Context(), "SELECT * FROM items", nil)
	if err != nil {
		t.Fatal(err)
	}
	if len(r.Columns()) != 3 {
		t.Fatal(r.Columns())
	}
	r.Close()
	connExec(t, c, "ALTER TABLE items ADD COLUMN flag BOOLEAN")
	r, err = c.QueryContext(t.Context(), "SELECT * FROM items", nil)
	if err != nil {
		t.Fatal(err)
	}
	if len(r.Columns()) != 4 {
		t.Fatal(r.Columns())
	}
	r.Close()
	if _, err := c.ExecContext(t.Context(), "INSERT INTO items VALUES(?,?,?,?)", nativeArgs([]any{int64(1), nil, nil, nil})); !errors.Is(err, ErrConstraintPrimaryKey) {
		t.Fatal(err)
	}
	if c.cache.entries["INSERT INTO items VALUES(?,?,?,?)"] != nil {
		t.Fatal("failed statement retained")
	}
	if _, err := c.ExecContext(t.Context(), "INSERT INTO items(id) VALUES(?)", nil); err == nil {
		t.Fatal("accepted missing bindings")
	}
	if c.cache.entries["INSERT INTO items(id) VALUES(?)"] != nil {
		t.Fatal("failed bind retained")
	}
	connExec(t, c, "SELECT 1; SELECT 2")
	for key := range c.cache.entries {
		if strings.Contains(key, ";") {
			t.Fatal("script cached", key)
		}
	}
	if err := c.Close(); err != nil {
		t.Fatal(err)
	}
	if len(c.stmts) != 0 || c.cache.lru.Len() != 0 {
		t.Fatal("close retained native statements")
	}
}

func TestStatementCacheOptionsAndInit(t *testing.T) {
	for _, dsn := range []string{":memory:?_stmt_cache_size=-1", ":memory:?_stmt_cache_size=1025", ":memory:?_stmt_cache_size=x"} {
		if _, err := NewConnector(dsn); err == nil {
			t.Fatal(dsn)
		}
	}
	connector := testConnector(t, ":memory:?_stmt_cache_size=4", WithStatementCache(2), WithInit(func(ctx context.Context, c *Conn) error {
		_, err := c.ExecContext(ctx, "SELECT 42", nil)
		return err
	}))
	dc, err := connector.Connect(t.Context())
	if err != nil {
		t.Fatal(err)
	}
	defer dc.Close()
	c := dc.(*Conn)
	if c.cfg.statementCache != 2 || len(c.stmts) != 0 {
		t.Fatal("precedence/init cache", c.cfg.statementCache, len(c.stmts))
	}
	plain := physicalConn(t)
	connExec(t, plain, "SELECT 1")
	if plain.cache != nil || len(plain.stmts) != 0 {
		t.Fatal("cache enabled by default")
	}
}

func TestStatementCacheReuseAndEviction(t *testing.T) {
	c := physicalConn(t, WithStatementCache(2))
	connRow(t, c, "SELECT ?", int64(1))
	ptr := c.cache.entries["SELECT ?"].Value.(*stmt).ptr
	r, err := c.QueryContext(t.Context(), "SELECT ?", nativeArgs([]any{int64(2)}))
	if err != nil {
		t.Fatal(err)
	}
	if r.(*rows).s.ptr != ptr {
		t.Fatal("cache hit prepared a new native statement")
	}
	value := make([]driver.Value, 1)
	if err := r.Next(value); err != nil || value[0] != int64(2) {
		t.Fatal(value, err)
	}
	r.Close()
	connRow(t, c, "SELECT 2")
	connRow(t, c, "SELECT ?", int64(3))
	connRow(t, c, "SELECT 3")
	if c.cache.entries["SELECT 2"] != nil || c.cache.entries["SELECT ?"] == nil {
		t.Fatal("incorrect LRU eviction")
	}
}

func TestHooksAuthorizerTrace(t *testing.T) {
	baseline := ffi.CallbackCount()
	c := physicalConn(t, WithStatementCache(4))
	connExec(t, c, "CREATE TABLE data(id INTEGER PRIMARY KEY, value)")
	var updates []int64
	commits, rollbacks := 0, 0
	if err := c.RegisterUpdateHook(func(op int, db, table string, id int64) {
		if op != 18 || db != "main" || table != "data" {
			t.Errorf("update: %d %s %s", op, db, table)
		}
		updates = append(updates, id)
		if _, err := c.AutoCommit(); !errors.Is(err, ErrCallbackReentry) {
			t.Error(err)
		}
	}); err != nil {
		t.Fatal(err)
	}
	if err := c.RegisterCommitHook(func() int { commits++; return 0 }); err != nil {
		t.Fatal(err)
	}
	if err := c.RegisterRollbackHook(func() { rollbacks++ }); err != nil {
		t.Fatal(err)
	}
	connExec(t, c, "INSERT INTO data VALUES(1099511627777,'ok')") // Exercises the 64-bit callback ABI on MIPS.
	connExec(t, c, "BEGIN; INSERT INTO data VALUES(2,'rollback'); ROLLBACK")
	if !slices.Equal(updates, []int64{1099511627777, 2}) || commits != 1 || rollbacks != 1 {
		t.Fatal(updates, commits, rollbacks)
	}
	if err := c.RegisterCommitHook(func() int { return 1 }); err != nil {
		t.Fatal(err)
	}
	if _, err := c.ExecContext(t.Context(), "INSERT INTO data VALUES(3,'denied')", nil); !errors.Is(err, ErrConstraintCommitHook) {
		t.Fatal(err)
	}
	if value := connRow(t, c, "SELECT count(*) FROM data")[0]; value != int64(1) {
		t.Fatal(value)
	}
	if err := c.RegisterCommitHook(nil); err != nil {
		t.Fatal(err)
	}
	if c.api.SetAuthorizer != nil {
		if err := c.RegisterAuthorizer(func(a Authorization) AuthResult {
			if a.Action == 20 && a.Argument1 == "data" && a.Argument2 == "value" {
				return AuthIgnore
			}
			if a.Action == 9 {
				return AuthDeny
			}
			return AuthAllow
		}); err != nil {
			t.Fatal(err)
		}
		if value := connRow(t, c, "SELECT value FROM data")[0]; value != nil {
			t.Fatal("authorization bypassed", value)
		}
		if _, err := c.ExecContext(t.Context(), "DELETE FROM data", nil); !errors.Is(err, ErrAuth) {
			t.Fatal(err)
		}
		if err := c.RegisterAuthorizer(nil); err != nil {
			t.Fatal(err)
		}
		if value := connRow(t, c, "SELECT value FROM data")[0]; value != "ok" {
			t.Fatal(value)
		}
	}
	if c.api.TraceV2 != nil {
		var events []TraceEvent
		if err := c.SetTrace(&TraceConfig{Mask: TraceStatement | TraceProfile | TraceRow | TraceClose, Callback: func(e TraceEvent) { events = append(events, e) }}); err != nil {
			t.Fatal(err)
		}
		connRow(t, c, "SELECT ? AS value", "private-value")
		for _, e := range events {
			if strings.Contains(e.SQL, "private-value") || e.ExpandedSQL != "" {
				t.Fatal("trace expanded by default", e)
			}
		}
		if len(events) < 3 {
			t.Fatal(events)
		}
		if c.api.ExpandedSQL != nil {
			if err := c.SetTrace(&TraceConfig{Mask: TraceStatement | TraceClose, ExpandedSQL: true, Callback: func(e TraceEvent) { events = append(events, e) }}); err != nil {
				t.Fatal(err)
			}
			connRow(t, c, "SELECT ? AS value", "explicit-value")
			found := false
			for _, e := range events {
				if strings.Contains(e.ExpandedSQL, "explicit-value") {
					found = true
				}
			}
			if !found {
				t.Fatal(events)
			}
		}
		if err := c.Close(); err != nil {
			t.Fatal(err)
		}
		if events[len(events)-1].Event != TraceClose {
			t.Fatal(events)
		}
	} else {
		c.Close()
	}
	if ffi.CallbackCount() != baseline {
		t.Fatalf("callback leak %d -> %d", baseline, ffi.CallbackCount())
	}
}

func TestHookPanicsAndCleanup(t *testing.T) {
	c := physicalConn(t, WithStatementCache(2))
	connExec(t, c, "CREATE TABLE data(x)")
	for _, kind := range []string{"update", "commit", "rollback", "authorizer", "trace"} {
		t.Run(kind, func(t *testing.T) {
			panicFn := func() { panic("expected-" + kind) }
			var err error
			switch kind {
			case "update":
				err = c.RegisterUpdateHook(func(int, string, string, int64) { panicFn() })
			case "commit":
				err = c.RegisterCommitHook(func() int { panicFn(); return 0 })
			case "rollback":
				err = c.RegisterRollbackHook(panicFn)
			case "authorizer":
				if c.api.SetAuthorizer == nil {
					t.Skip("no authorizer")
				}
				err = c.RegisterAuthorizer(func(Authorization) AuthResult { panicFn(); return AuthAllow })
			case "trace":
				if c.api.TraceV2 == nil {
					t.Skip("no trace")
				}
				err = c.SetTrace(&TraceConfig{Mask: TraceStatement, Callback: func(TraceEvent) { panicFn() }})
			}
			if err != nil {
				t.Fatal(err)
			}
			query := "INSERT INTO data VALUES(1)"
			if kind == "rollback" {
				query = "BEGIN; INSERT INTO data VALUES(1); ROLLBACK"
			}
			_, err = c.ExecContext(t.Context(), query, nil)
			if err == nil || !strings.Contains(err.Error(), "expected-"+kind) {
				t.Fatal(err)
			}
			c.RegisterUpdateHook(nil)
			c.RegisterCommitHook(nil)
			c.RegisterRollbackHook(nil)
			if c.api.SetAuthorizer != nil {
				c.RegisterAuthorizer(nil)
			}
			if c.api.TraceV2 != nil {
				c.SetTrace(nil)
			}
			connExec(t, c, "SELECT 1")
		})
	}
	if c.api.TraceV2 != nil {
		if err := c.SetTrace(&TraceConfig{Mask: TraceClose, Callback: func(TraceEvent) { panic("close panic") }}); err != nil {
			t.Fatal(err)
		}
		if err := c.Close(); err == nil || !strings.Contains(err.Error(), "close panic") {
			t.Fatal(err)
		}
	}
	baseline := ffi.CallbackCount()
	for range 1000 {
		dc, err := testConnector(t, ":memory:").Connect(t.Context())
		if err != nil {
			t.Fatal(err)
		}
		conn := dc.(*Conn)
		if err := conn.RegisterUpdateHook(func(int, string, string, int64) {}); err != nil {
			t.Fatal(err)
		}
		if err := conn.RegisterUpdateHook(func(int, string, string, int64) {}); err != nil {
			t.Fatal(err)
		}
		if err := conn.Close(); err != nil {
			t.Fatal(err)
		}
	}
	if ffi.CallbackCount() != baseline {
		t.Fatal("hook registrations leaked")
	}
}

func TestAuthorizerOriginAndPreparedRecheck(t *testing.T) {
	c := physicalConn(t, WithStatementCache(4))
	if c.api.SetAuthorizer == nil {
		t.Skip("selected library omits authorization")
	}
	connExec(t, c, "CREATE TABLE secret(value); INSERT INTO secret VALUES('sensitive'); CREATE VIEW public_view AS SELECT value FROM secret")
	s, err := c.Prepare("SELECT value FROM secret")
	if err != nil {
		t.Fatal(err)
	}
	defer s.Close()
	connRow(t, c, "SELECT value FROM secret") // Populate a previously authorized cached program.
	foundOrigin := false
	if err := c.RegisterAuthorizer(func(a Authorization) AuthResult {
		if a.Action == 20 && a.Argument1 == "secret" {
			if a.Origin == "public_view" && a.Database == "main" && a.Argument2 == "value" {
				foundOrigin = true
			}
			return AuthIgnore
		}
		return AuthAllow
	}); err != nil {
		t.Fatal(err)
	}
	if value := connRow(t, c, "SELECT value FROM public_view")[0]; value != nil || !foundOrigin {
		t.Fatal(value, foundOrigin)
	}
	if err := c.RegisterAuthorizer(func(a Authorization) AuthResult {
		if a.Action == 20 && a.Argument1 == "secret" {
			return AuthDeny
		}
		return AuthAllow
	}); err != nil {
		t.Fatal(err)
	}
	if _, err := c.QueryContext(t.Context(), "SELECT value FROM secret", nil); !errors.Is(err, ErrAuth) {
		t.Fatal("cache bypassed authorizer", err)
	}
	if _, err := s.(driver.StmtQueryContext).QueryContext(t.Context(), nil); !errors.Is(err, ErrAuth) {
		t.Fatal("prepared statement bypassed authorizer", err)
	}
	if err := c.RegisterAuthorizer(nil); err != nil {
		t.Fatal(err)
	}
	connRow(t, c, "SELECT value FROM secret")
}

func TestTraceProfileCloseDoesNotLeakError(t *testing.T) {
	c := physicalConn(t)
	if c.api.TraceV2 == nil {
		t.Skip("selected library omits trace")
	}
	if err := c.SetTrace(&TraceConfig{Mask: TraceProfile, Callback: func(TraceEvent) { panic("profile-close") }}); err != nil {
		t.Fatal(err)
	}
	r, err := c.QueryContext(t.Context(), "SELECT 1 UNION ALL SELECT 2", nil)
	if err != nil {
		t.Fatal(err)
	}
	if err := r.Close(); err == nil || !strings.Contains(err.Error(), "profile-close") {
		t.Fatal(err)
	}
	if err := c.SetTrace(nil); err != nil {
		t.Fatal(err)
	}
	connRow(t, c, "SELECT 42")
}

func TestBackupCancellationRollsBackDestination(t *testing.T) {
	source, dest := physicalConn(t), physicalConn(t)
	connExec(t, source, "CREATE TABLE data(x); INSERT INTO data VALUES(zeroblob(50000))")
	connExec(t, dest, "CREATE TABLE previous(x); INSERT INTO previous VALUES(42)")
	ctx, cancel := context.WithCancel(t.Context())
	defer cancel()
	err := dest.BackupFrom(ctx, source, BackupOptions{PagesPerStep: 1, Progress: func(p BackupProgress) error {
		if p.Done {
			t.Fatal("fixture did not require multiple steps")
		}
		cancel()
		return nil
	}})
	if !errors.Is(err, context.Canceled) {
		t.Fatal(err)
	}
	if value := connRow(t, dest, "SELECT x FROM previous")[0]; value != int64(42) {
		t.Fatal(value)
	}
}

func TestDiagnosticsAndExtendedErrors(t *testing.T) {
	c := physicalConn(t)
	connExec(t, c, "PRAGMA foreign_keys=ON; CREATE TABLE parent(id INTEGER PRIMARY KEY, name TEXT UNIQUE NOT NULL CHECK(length(name)>0)); CREATE TABLE child(id REFERENCES parent(id))")
	connExec(t, c, "INSERT INTO parent VALUES(1,'one')")
	for _, tc := range []struct {
		sql  string
		want error
	}{
		{"INSERT INTO parent VALUES(1,'two')", ErrConstraintPrimaryKey},
		{"INSERT INTO parent VALUES(2,'one')", ErrConstraintUnique},
		{"INSERT INTO parent VALUES(2,NULL)", ErrConstraintNotNull},
		{"INSERT INTO parent VALUES(2,'')", ErrConstraintCheck},
		{"INSERT INTO child VALUES(9)", ErrConstraintForeignKey},
	} {
		_, err := c.ExecContext(t.Context(), tc.sql, nil)
		if !errors.Is(err, tc.want) || !errors.Is(err, ErrConstraint) {
			t.Fatalf("%s: %v", tc.sql, err)
		}
	}
	if filename, err := c.GetFilename(""); err != nil || filename != "" {
		t.Fatal(filename, err)
	}
	if _, err := c.GetFilename("missing"); err == nil {
		t.Fatal("accepted missing schema")
	}
	if auto, err := c.AutoCommit(); err != nil || !auto {
		t.Fatal(auto, err)
	}
	connExec(t, c, "BEGIN")
	if auto, err := c.AutoCommit(); err != nil || auto {
		t.Fatal(auto, err)
	}
	connExec(t, c, "ROLLBACK")
	connector := testConnector(t, filepath.Join(t.TempDir(), "missing", "db.sqlite"))
	_, err := connector.Connect(t.Context())
	if !errors.Is(err, ErrCantOpen) {
		t.Fatal(err)
	}
	e, ok := errors.AsType[*Error](err)
	if !ok {
		t.Fatal(err)
	}
	if runtime.GOOS != "windows" && connector.api.SystemErrno != nil && e.SystemErrno != syscall.ENOENT {
		t.Fatalf("errno: %v", err)
	}
	if e.SystemErrno != 0 && !errors.Is(err, e.SystemErrno) {
		t.Fatal("errno not unwrapped", err)
	}
	if len(connector.SourceID()) < 40 {
		t.Fatal(connector.SourceID())
	}
}

func TestBackupOppositeDirectionsNoDeadlock(t *testing.T) {
	a, b := physicalConn(t), physicalConn(t)
	connExec(t, a, "CREATE TABLE data(x); INSERT INTO data VALUES(1)")
	connExec(t, b, "CREATE TABLE data(x); INSERT INTO data VALUES(2)")
	var wg sync.WaitGroup
	for _, pair := range [][2]*Conn{{a, b}, {b, a}} {
		wg.Go(func() {
			if err := pair[0].BackupFrom(t.Context(), pair[1], BackupOptions{}); err != nil {
				t.Error(err)
			}
		})
	}
	wg.Wait()
}
