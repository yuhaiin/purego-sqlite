package sqlite

import "fmt"

// GetFilename returns SQLite's filename for a schema (empty selects main).
// In-memory and TEMP schemas have no persistent filename. Unknown schemas fail.
func (c *Conn) GetFilename(schema string) (string, error) {
	if c.inCallback.Load() != 0 {
		return "", ErrCallbackReentry
	}
	schema, err := schemaName(schema)
	if err != nil {
		return "", err
	}
	c.mu.Lock()
	defer c.mu.Unlock()
	if c.db == 0 {
		return "", ErrClosed
	}
	if c.api.DBReadonly(c.db, schema) < 0 {
		return "", fmt.Errorf("sqlite: unknown schema %q", schema)
	}
	return c.api.DBFilename(c.db, schema), nil
}

// AutoCommit reports SQLite's native transaction state, including transactions
// started with SQL rather than database/sql.BeginTx.
func (c *Conn) AutoCommit() (bool, error) {
	if c.inCallback.Load() != 0 {
		return false, ErrCallbackReentry
	}
	c.mu.Lock()
	defer c.mu.Unlock()
	if c.db == 0 {
		return false, ErrClosed
	}
	return c.api.Autocommit(c.db) != 0, nil
}
