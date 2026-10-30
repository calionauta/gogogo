// SCOPE:core — Platform queue driver selection (ncruces on Android).

//go:build android

package queue

import _ "github.com/ncruces/go-sqlite3/driver"

// sqlDriver is the database/sql driver ncruces registers on Android, where
// tursogo has no embedded library.
const sqlDriver = "sqlite3"

// queueDSN carries the same busy_timeout in the ncruces spelling.
func queueDSN(path string) string {
	return path + "?_pragma=busy_timeout(10000)&_pragma=journal_mode(WAL)"
}
