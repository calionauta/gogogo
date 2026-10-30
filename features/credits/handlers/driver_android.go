// SCOPE:feature — Platform ledger driver selection (ncruces on Android).

//go:build android

package handlers

import _ "github.com/ncruces/go-sqlite3/driver"

const ledgerDriver = "sqlite3"

func ledgerDSN(path string) string {
	return path + "?_pragma=busy_timeout(10000)&_pragma=journal_mode(WAL)"
}
