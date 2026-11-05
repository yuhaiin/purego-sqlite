package sqlite

import (
	"errors"
	"fmt"
	"time"
	"unsafe"

	"github.com/yuhaiin/purego-sqlite/internal/ffi"
)

const (
	hookUpdate = iota
	hookCommit
	hookRollback
	hookAuthorizer
	hookTrace
	hookCount
)

// All hooks run synchronously on the owning physical connection. Install them
// with WithInit for pooled use. Callbacks must not use that connection or pool.
// A nil callback removes the corresponding registration.
func (c *Conn) replaceHook(kind int, enabled bool, handlers ffi.CallbackHandlers, install func(ffi.CallbackPointers, uintptr) int32) error {
	if c.inCallback.Load() != 0 {
		return ErrCallbackReentry
	}
	c.mu.Lock()
	defer c.mu.Unlock()
	if err := c.available(); err != nil {
		return err
	}
	var pointers ffi.CallbackPointers
	var token uintptr
	if enabled {
		var err error
		pointers, err = c.api.Callbacks()
		if err != nil {
			return err
		}
		token, err = ffi.NewCallbackToken(handlers)
		if err != nil {
			return err
		}
	}
	// Changing authorization must never reuse a previously authorized program.
	if kind == hookAuthorizer {
		c.clearStatementCacheLocked()
	}
	if rc := install(pointers, token); rc != 0 {
		ffi.ReleaseCallbackToken(token)
		return c.sqliteError(rc)
	}
	ffi.ReleaseCallbackToken(c.hookTokens[kind])
	c.hookTokens[kind] = token
	return nil
}

func (c *Conn) invokeHook(fn func() int32, fallback int32) (result int32) {
	c.inCallback.Add(1)
	defer c.inCallback.Add(-1)
	defer func() {
		if value := recover(); value != nil {
			c.callbackErr = errors.Join(c.callbackErr, fmt.Errorf("sqlite: hook panic: %v", value))
			result = fallback
		}
	}()
	return fn()
}

// RegisterUpdateHook reports SQLITE_INSERT (18), SQLITE_UPDATE (23), or
// SQLITE_DELETE (9), database, table, and rowid. This is not complete CDC:
// WITHOUT ROWID tables, REPLACE deletions and truncate optimizations may be absent.
// Panics become errors on the invoking operation; already applied writes are
// not undone. Use an explicit transaction when the caller needs rollback.
func (c *Conn) RegisterUpdateHook(fn func(operation int, database, table string, rowID int64)) error {
	return c.replaceHook(hookUpdate, fn != nil, ffi.CallbackHandlers{Update: func(op int32, db, table uintptr, rowID int64) {
		c.invokeHook(func() int32 { fn(int(op), ffi.NativeString(db), ffi.NativeString(table), rowID); return 0 }, 0)
	}}, func(p ffi.CallbackPointers, token uintptr) int32 { c.api.UpdateHook(c.db, p.Update, token); return 0 })
}

// RegisterCommitHook installs a commit veto: zero permits commit, nonzero causes
// rollback. A panic rejects the commit and is reported as an operation error.
func (c *Conn) RegisterCommitHook(fn func() int) error {
	return c.replaceHook(hookCommit, fn != nil, ffi.CallbackHandlers{Commit: func() int32 {
		return c.invokeHook(func() int32 {
			if fn() != 0 {
				return 1
			}
			return 0
		}, 1)
	}}, func(p ffi.CallbackPointers, token uintptr) int32 { c.api.CommitHook(c.db, p.Commit, token); return 0 })
}

func (c *Conn) RegisterRollbackHook(fn func()) error {
	return c.replaceHook(hookRollback, fn != nil, ffi.CallbackHandlers{Rollback: func() {
		c.invokeHook(func() int32 { fn(); return 0 }, 0)
	}}, func(p ffi.CallbackPointers, token uintptr) int32 {
		c.api.RollbackHook(c.db, p.Rollback, token)
		return 0
	})
}

// Authorization describes SQLite's prepare/reprepare authorization request.
// Argument1/Argument2 depend on Action; Origin names the invoking trigger/view.
// Strings are copied and remain valid after the callback returns.
type Authorization struct {
	Action                                 int
	Argument1, Argument2, Database, Origin string
}

type AuthResult int32

const (
	AuthAllow  AuthResult = 0
	AuthDeny   AuthResult = 1
	AuthIgnore AuthResult = 2
)

// RegisterAuthorizer preserves all SQLite authorizer metadata. Panics and invalid
// return values deny access. AuthIgnore has action-specific SQLite semantics;
// for example, it replaces a denied column read with NULL.
func (c *Conn) RegisterAuthorizer(fn func(Authorization) AuthResult) error {
	if c.api.SetAuthorizer == nil {
		return fmt.Errorf("%w: authorizer", ErrUnsupported)
	}
	return c.replaceHook(hookAuthorizer, fn != nil, ffi.CallbackHandlers{Authorize: func(op int32, a, b, db, origin uintptr) int32 {
		return c.invokeHook(func() int32 {
			result := fn(Authorization{int(op), ffi.NativeString(a), ffi.NativeString(b), ffi.NativeString(db), ffi.NativeString(origin)})
			if result != AuthAllow && result != AuthDeny && result != AuthIgnore {
				return int32(AuthDeny)
			}
			return int32(result)
		}, int32(AuthDeny))
	}}, func(p ffi.CallbackPointers, token uintptr) int32 {
		return c.api.SetAuthorizer(c.db, p.Authorize, token)
	})
}

type TraceMask uint32

const (
	TraceStatement TraceMask = 1
	TraceProfile   TraceMask = 2
	TraceRow       TraceMask = 4
	TraceClose     TraceMask = 8
)

// TraceEvent owns copies of SQL text. Runtime is populated for TraceProfile.
// SQL contains placeholders unless ExpandedSQL was explicitly requested. Trigger
// statement events may contain SQLite's comment describing the trigger instead.
type TraceEvent struct {
	Event            TraceMask
	SQL, ExpandedSQL string
	Runtime          time.Duration
}

type TraceConfig struct {
	Mask        TraceMask
	Callback    func(TraceEvent)
	ExpandedSQL bool
}

// SetTrace replaces tracing; nil or a zero mask disables it. ExpandedSQL is
// opt-in because parameter values can contain secrets. Callback panics are
// reported on the invoking operation, including Close, without crossing FFI.
func (c *Conn) SetTrace(config *TraceConfig) error {
	if c.api.TraceV2 == nil {
		return fmt.Errorf("%w: trace", ErrUnsupported)
	}
	var cfg TraceConfig
	if config != nil {
		cfg = *config
	}
	if cfg.Mask & ^(TraceStatement|TraceProfile|TraceRow|TraceClose) != 0 {
		return errors.New("sqlite: invalid trace mask")
	}
	if cfg.Mask != 0 && cfg.Callback == nil {
		return errors.New("sqlite: trace callback is nil")
	}
	if cfg.ExpandedSQL && c.api.ExpandedSQL == nil {
		return fmt.Errorf("%w: expanded SQL", ErrUnsupported)
	}
	return c.replaceHook(hookTrace, cfg.Mask != 0, ffi.CallbackHandlers{Trace: func(mask uint32, p, x uintptr) {
		c.invokeHook(func() int32 {
			event := TraceEvent{Event: TraceMask(mask)}
			switch event.Event {
			case TraceStatement:
				event.SQL = ffi.NativeString(x)
			case TraceProfile, TraceRow:
				event.SQL = c.api.SQL(p)
			}
			if event.Event == TraceProfile && x != 0 {
				ns := *(*uint64)(unsafe.Pointer(ffi.NativePointer(x)))
				event.Runtime = time.Duration(min(ns, uint64(1<<63-1)))
			}
			if cfg.ExpandedSQL && event.Event == TraceStatement {
				text := c.api.ExpandedSQL(p)
				if text != nil {
					event.ExpandedSQL = ffi.NativeString(uintptr(unsafe.Pointer(text)))
					c.api.Free(text)
				}
			}
			// CLOSE does not inspect the closing sqlite3* or its statements.
			cfg.Callback(event)
			return 0
		}, 0)
	}}, func(p ffi.CallbackPointers, token uintptr) int32 {
		return c.api.TraceV2(c.db, uint32(cfg.Mask), p.Trace, token)
	})
}

func (c *Conn) takeCallbackError(err error) error {
	if c.callbackErr == nil {
		return err
	}
	err = errors.Join(err, c.callbackErr)
	c.callbackErr = nil
	return err
}
