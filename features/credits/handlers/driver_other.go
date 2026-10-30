// SCOPE:feature — Platform ledger driver selection (tursogo).

//go:build !android

package handlers

import _ "turso.tech/database/tursogo"

const ledgerDriver = "turso"

func ledgerDSN(path string) string { return path + "?_busy_timeout=10000" }
