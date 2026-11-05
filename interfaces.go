package sqlite

import "database/sql/driver"

var (
	_ driver.Driver                         = (*Driver)(nil)
	_ driver.DriverContext                  = (*Driver)(nil)
	_ driver.Connector                      = (*Connector)(nil)
	_ driver.Conn                           = (*Conn)(nil)
	_ driver.ConnPrepareContext             = (*Conn)(nil)
	_ driver.ConnBeginTx                    = (*Conn)(nil)
	_ driver.ExecerContext                  = (*Conn)(nil)
	_ driver.QueryerContext                 = (*Conn)(nil)
	_ driver.Pinger                         = (*Conn)(nil)
	_ driver.SessionResetter                = (*Conn)(nil)
	_ driver.Validator                      = (*Conn)(nil)
	_ driver.Stmt                           = (*stmt)(nil)
	_ driver.StmtExecContext                = (*stmt)(nil)
	_ driver.StmtQueryContext               = (*stmt)(nil)
	_ driver.Rows                           = (*rows)(nil)
	_ driver.RowsColumnTypeDatabaseTypeName = (*rows)(nil)
	_ driver.RowsColumnTypeNullable         = (*rows)(nil)
	_ driver.RowsColumnTypeScanType         = (*rows)(nil)
	_ driver.Tx                             = (*transaction)(nil)
	_ driver.Result                         = result{}
)
