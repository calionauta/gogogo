// SCOPE:core — request user-agent captured with the session row.
//
// The account page lists sessions so the owner can spot a device that is not
// theirs. A UUID alone cannot answer that; the user-agent is the smallest
// honest hint. It is never an authorization input, never leaves the owner's
// page, and expires with the session.
package auth

import (
	"context"
	"strings"
)

type userAgentKey struct{}

// maxUserAgentLen bounds what a client can make the database store. Real
// browser strings are under 250 bytes; anything longer is a client error or
// an attempt to use the session table as storage.
const maxUserAgentLen = 300

// WithUserAgent attaches the request's User-Agent to the context openSession
// reads. HTTP entry points call it; CLI/tests omit it and store an empty hint.
func WithUserAgent(ctx context.Context, raw string) context.Context {
	return context.WithValue(ctx, userAgentKey{}, cleanUserAgent(raw))
}

func userAgentFrom(ctx context.Context) string {
	ua, _ := ctx.Value(userAgentKey{}).(string)
	return ua
}

// cleanUserAgent strips control characters (a header may carry them) and caps
// the value. It does not parse: the raw string is the evidence, the page
// decides how to label it.
func cleanUserAgent(raw string) string {
	raw = strings.TrimSpace(raw)
	raw = strings.Map(func(r rune) rune {
		if r < 0x20 || r == 0x7f {
			return ' '
		}
		return r
	}, raw)
	if len(raw) > maxUserAgentLen {
		raw = raw[:maxUserAgentLen]
	}
	return strings.TrimSpace(raw)
}
