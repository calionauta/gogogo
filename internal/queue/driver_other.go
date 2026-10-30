// SCOPE:core — Platform queue driver selection (tursogo).

//go:build !android

package queue

import _ "turso.tech/database/tursogo"

// sqlDriver is the database/sql driver tursogo registers.
const sqlDriver = "turso"

// queueDSN carries busy_timeout in the DSN (a PRAGMA statement is not applied
// by the driver on open), matching the app DB's DSN in internal/db.
func queueDSN(path string) string { return path + "?_busy_timeout=10000" }
