package migrations

import (
	"errors"
	"fmt"

	"modernc.org/libc"
	sqlite3 "modernc.org/sqlite/lib"
)

// sqliteComplete asks SQLite whether statement forms one complete SQL statement.
func sqliteComplete(statement string) (bool, error) {
	tls := libc.NewTLS()
	defer tls.Close()

	cString, err := libc.CString(statement)
	if err != nil {
		return false, fmt.Errorf("allocate SQL text: %w", err)
	}
	defer libc.Xfree(tls, cString)

	switch sqlite3.Xsqlite3_complete(tls, cString) {
	case 0:
		return false, nil
	case 1:
		return true, nil
	default:
		return false, errors.New("sqlite3_complete returned an unexpected result")
	}
}
