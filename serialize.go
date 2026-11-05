package sqlite

import (
	"context"
	"errors"
	"fmt"
	"math"
	"strings"
	"unsafe"
)

func schemaName(schema string) (string, error) {
	if strings.ContainsRune(schema, 0) {
		return "", errors.New("sqlite: schema contains NUL")
	}
	if schema == "" {
		schema = "main"
	}
	return schema, nil
}

// Serialize returns an independent database image for schema (empty means main).
// The selected SQLite library must export sqlite3_serialize.
func (c *Conn) Serialize(schema string) ([]byte, error) {
	return c.SerializeContext(context.Background(), schema)
}

// SerializeContext copies the image before releasing SQLite's allocation.
func (c *Conn) SerializeContext(ctx context.Context, schema string) (data []byte, err error) {
	if c.inCallback.Load() != 0 {
		return nil, ErrCallbackReentry
	}
	schema, err = schemaName(schema)
	if err != nil {
		return nil, err
	}
	c.mu.Lock()
	defer c.mu.Unlock()
	if err = c.available(); err != nil {
		return nil, err
	}
	if c.api.Serialize == nil {
		return nil, fmt.Errorf("%w: serialize", ErrUnsupported)
	}
	err = c.runLocked(ctx, func() error {
		if c.api.DBReadonly(c.db, schema) < 0 {
			return fmt.Errorf("sqlite: unknown schema %q", schema)
		}
		var size int64
		p := c.api.Serialize(c.db, schema, &size, 0)
		if p != nil {
			defer c.api.Free(p)
		}
		if p == nil {
			if size == 0 && c.api.Errcode(c.db) == 0 {
				return nil
			}
			if rc := c.api.Errcode(c.db); rc != 0 && rc != 100 && rc != 101 {
				return c.sqliteError(rc)
			}
			return errors.New("sqlite: serialize failed to produce a database image")
		}
		if size < 0 || uint64(size) > uint64(math.MaxInt) {
			return errors.New("sqlite: serialized image exceeds Go slice size")
		}
		data = append([]byte(nil), unsafe.Slice(p, int(size))...)
		return nil
	})
	if err != nil {
		return nil, err
	}
	return data, nil
}

// Deserialize replaces schema with a private, writable in-memory copy of data.
// It does not modify the caller's slice or the original database file. Close
// prepared statements and end transactions first. TEMP is unsupported by SQLite.
// WAL images are rejected; use online backup to copy a live WAL database.
func (c *Conn) Deserialize(data []byte, schema string) error {
	return c.DeserializeContext(context.Background(), data, schema)
}

func (c *Conn) DeserializeContext(ctx context.Context, data []byte, schema string) error {
	if c.inCallback.Load() != 0 {
		return ErrCallbackReentry
	}
	schema, err := schemaName(schema)
	if err != nil {
		return err
	}
	if strings.EqualFold(schema, "temp") {
		return errors.New("sqlite: cannot deserialize into temp")
	}
	if len(data) < 100 || string(data[:16]) != "SQLite format 3\x00" {
		return errors.New("sqlite: invalid database image header")
	}
	if data[18] != 1 || data[19] != 1 {
		return errors.New("sqlite: deserialize requires a rollback-journal image; use online backup for WAL databases")
	}
	if len(data) > math.MaxInt-20 {
		return errors.New("sqlite: database image too large")
	}
	c.mu.Lock()
	defer c.mu.Unlock()
	if err := c.available(); err != nil {
		return err
	}
	if c.api.Deserialize == nil {
		return fmt.Errorf("%w: deserialize", ErrUnsupported)
	}
	if c.tx != nil || c.api.Autocommit(c.db) == 0 {
		return errors.New("sqlite: deserialize requires no active transaction")
	}
	c.clearStatementCacheLocked()
	if len(c.stmts) != 0 {
		return errors.New("sqlite: close prepared statements before deserialize")
	}
	return c.runLocked(ctx, func() error {
		if c.api.DBReadonly(c.db, schema) < 0 {
			return fmt.Errorf("sqlite: unknown schema %q", schema)
		}
		// SQLite recommends padding potentially malformed images. FREEONCLOSE
		// transfers ownership even when sqlite3_deserialize returns an error.
		size := len(data) + 20
		p := c.api.Malloc64(uint64(size))
		if p == nil {
			return c.sqliteError(7)
		}
		buffer := unsafe.Slice(p, size)
		copy(buffer, data)
		clear(buffer[len(data):])
		return c.sqliteError(c.api.Deserialize(c.db, schema, p, int64(len(data)), int64(size), 1|2))
	})
}
