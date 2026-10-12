// SCOPE:core — ID and timestamp helpers shared by every store.
// Lives in dbschema (not internal/db) so driver-free targets share it
// without linking tursogo.
package dbschema

import (
	"time"

	"github.com/google/uuid"
)

// TimeFormat is the layout of every TEXT timestamp column. Fixed-offset UTC
// keeps lexicographic ordering equal to chronological ordering, which is what
// the ORDER BY created clauses rely on, and stays readable in a raw sqlite3
// shell.
const TimeFormat = "2006-01-02T15:04:05.000Z07:00"

// NewID returns a time-ordered UUIDv7 as text.
//
// IDs are generated here rather than by the engine so the value exists before
// the write (it can be logged, put in a response, or used as a subject) and so
// the same generator works for every table. uuid.NewV7 only fails when the
// system entropy source does, and a random v4 is still a valid primary key, so
// the fallback keeps a caller from having to handle an impossible error.
func NewID() string {
	id, err := uuid.NewV7()
	if err != nil {
		return uuid.NewString()
	}
	return id.String()
}

// Now returns the current UTC time in TimeFormat.
func Now() string {
	return time.Now().UTC().Format(TimeFormat)
}
