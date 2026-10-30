// SCOPE:core — Platform database driver selection (tursogo).

//go:build !android

package db

import (
	"fmt"
	"strings"

	_ "turso.tech/database/tursogo"
)

// sqlDriver is the database/sql driver tursogo registers.
const sqlDriver = "turso"

// localDSN builds the `<path>?<options>` DSN tursogo parses. The DSN is the
// only place a connection option survives database/sql pooling: per-connection
// PRAGMAs run once on a pooled connection and are lost on the next one, so
// anything the engine accepts as an option belongs here.
func localDSN(path string) string {
	if strings.ContainsAny(path, "?") {
		return path
	}
	return fmt.Sprintf("%s?_busy_timeout=%d", path, busyTimeoutMS)
}
