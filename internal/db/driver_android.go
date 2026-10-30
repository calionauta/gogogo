// SCOPE:core — Platform database driver selection (ncruces on Android).

//go:build android

package db

import (
	"fmt"
	"strings"

	_ "github.com/ncruces/go-sqlite3/driver"
)

// sqlDriver is the database/sql driver ncruces registers. Android uses the
// pure-Go SQLite driver because tursogo embeds no Android library.
const sqlDriver = "sqlite3"

// localDSN carries the same pragmas as the desktop DSN, in the ncruces
// spelling (`_pragma=name(value)`), so every pooled connection gets them.
func localDSN(path string) string {
	if strings.ContainsAny(path, "?") {
		return path
	}
	return fmt.Sprintf(
		"%s?_pragma=busy_timeout(%d)&_pragma=journal_mode(WAL)&_pragma=foreign_keys(ON)",
		path, busyTimeoutMS,
	)
}
