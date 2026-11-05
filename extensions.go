package sqlite

import (
	"context"
	"database/sql"
	"errors"
	"path/filepath"
	"strings"
	"unsafe"
)

// LoadExtension loads a trusted native extension into this exact physical
// connection. Call inside WithInit for every pooled connection, or sql.Conn.Raw.
// Loading arbitrary native code grants it the privileges of this process.
func (c *Conn) LoadExtension(ctx context.Context, path, entry string) error {
	if c.inCallback.Load() != 0 {
		return ErrCallbackReentry
	}
	if !filepath.IsAbs(path) || strings.ContainsRune(path, 0) || strings.ContainsRune(entry, 0) {
		return errors.New("sqlite: extension path must be absolute and path/entry contain no NUL")
	}
	if entry == "" {
		entry = "sqlite3_extension_init"
	}
	c.mu.Lock()
	defer c.mu.Unlock()
	if err := c.available(); err != nil {
		return err
	}
	if !c.cfg.extensions {
		return ErrExtensionDisabled
	}
	if c.api.ConfigExtension == nil || c.api.LoadExtension == nil {
		return ErrUnsupported
	}
	return c.runLocked(ctx, func() (err error) {
		if rc := c.api.ConfigExtension(c.db, 1); rc != 0 {
			return c.sqliteError(rc)
		}
		defer func() { err = errors.Join(err, c.sqliteError(c.api.ConfigExtension(c.db, 0))) }()
		var message *byte
		rc := c.api.LoadExtension(c.db, path, entry, &message)
		if rc == 0 {
			if message != nil {
				c.api.Free(message)
			}
			return nil
		}
		nativeErr := c.sqliteError(rc)
		if message != nil {
			// SQLite owns the NUL-terminated error allocation; copy then free it.
			n := 0
			for *(*byte)(unsafe.Add(unsafe.Pointer(message), n)) != 0 {
				n++
			}
			text := string(unsafe.Slice(message, n))
			c.api.Free(message)
			e := nativeErr.(*Error)
			e.Message = text
		}
		return nativeErr
	})
}

// LoadExtension targets a reserved sql.Conn, never an unspecified sql.DB pool.
func LoadExtension(ctx context.Context, conn *sql.Conn, path, entry string) error {
	return conn.Raw(func(dc any) error {
		c, ok := dc.(*Conn)
		if !ok {
			return errors.New("sqlite: physical connection belongs to another driver")
		}
		return c.LoadExtension(ctx, path, entry)
	})
}
