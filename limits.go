package sqlite

import (
	"fmt"
	"math"
)

// LimitCategory identifies a public sqlite3_limit resource limit.
type LimitCategory int32

const (
	LimitLength LimitCategory = iota
	LimitSQLLength
	LimitColumn
	LimitExprDepth
	LimitCompoundSelect
	LimitVDBEOp
	LimitFunctionArg
	LimitAttached
	LimitLikePatternLength
	LimitVariableNumber
	LimitTriggerDepth
	LimitWorkerThreads
	LimitParserDepth
	LimitSchema
	LimitTriggerSteps
)

// SetLimit changes a resource limit on this physical connection and returns its
// previous value. Any negative value queries without changing the limit. SQLite
// clamps increases to its compile-time maximum. Call inside WithInit or Conn.Raw;
// limits belong to the connection, not a database/sql pool or database file.
// Categories absent from the loaded SQLite version return ErrUnsupported.
func (c *Conn) SetLimit(category LimitCategory, value int) (int, error) {
	if c.inCallback.Load() != 0 {
		return 0, ErrCallbackReentry
	}
	if category < LimitLength {
		return 0, fmt.Errorf("sqlite: invalid limit category %d", category)
	}
	if value > math.MaxInt32 {
		return 0, fmt.Errorf("sqlite: limit value %d exceeds a C int", value)
	}
	if value < 0 {
		value = -1
	}
	c.mu.Lock()
	defer c.mu.Unlock()
	if err := c.available(); err != nil {
		return 0, err
	}
	previous := c.api.Limit(c.db, int32(category), int32(value))
	if previous < 0 {
		return 0, fmt.Errorf("%w: SQLite limit category %d", ErrUnsupported, category)
	}
	return int(previous), nil
}
