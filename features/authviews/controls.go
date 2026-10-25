// SCOPE:core — view helpers for the account affordance in controls.templ.
package authviews

import (
	"strings"
	"unicode/utf8"
)

// accountInitial is the identity disc's one-letter fallback: the first
// character of the address, uppercased. A rune, not a byte — "émile@example.com"
// must not render half of a multi-byte sequence, and the empty address (guest)
// renders no disc at all, so the "?" is unreachable but keeps the function
// total.
func accountInitial(email string) string {
	r, size := utf8.DecodeRuneInString(email)
	if size == 0 {
		return "?"
	}
	return strings.ToUpper(string(r))
}
