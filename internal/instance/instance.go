// SCOPE:core — stable instance identity (durable names + event origin)
//
// Every process that participates in cross-instance fan-out needs ONE
// identity that survives a restart: it names the JetStream durable (one
// consumer per instance, or only one instance would ever converge) and
// tags published events so an instance can ignore its own echo.
//
// Convention over configuration: APP_INSTANCE_ID when the operator needs a
// stable id (StatefulSet, fixed hostnames), the hostname otherwise, and a
// random fallback only when the OS reports no hostname. Nothing to
// configure for a single-instance deployment.
package instance

import (
	"crypto/rand"
	"encoding/hex"
	"os"
	"strings"
	"sync"
)

// EnvVar overrides the derived identity. Optional by design.
const EnvVar = "APP_INSTANCE_ID"

// maxLen keeps the id usable in a JetStream durable name; NATS caps durable
// names and the suffix must stay readable next to the app name.
const maxLen = 32

var (
	once sync.Once
	id   string
)

// ID returns this process's stable identity. It is computed once and cached:
// the durable name and every published event must agree for the process's
// whole lifetime.
func ID() string {
	once.Do(func() {
		id = sanitize(os.Getenv(EnvVar))
		if id == "" {
			if host, err := os.Hostname(); err == nil {
				id = sanitize(host)
			}
		}
		if id == "" {
			var b [4]byte
			if _, err := rand.Read(b[:]); err == nil {
				id = "instance-" + hex.EncodeToString(b[:])
			} else {
				id = "instance-local"
			}
		}
	})
	return id
}

// sanitize lowercases and reduces the identity to [a-z0-9-], which every
// transport this feeds accepts (NATS durable names, log fields, headers).
// Runs of other characters collapse to one dash; the result is bounded and
// never starts or ends with a dash.
func sanitize(s string) string {
	s = strings.ToLower(strings.TrimSpace(s))
	var b strings.Builder
	dash := false
	for _, r := range s {
		ok := (r >= 'a' && r <= 'z') || (r >= '0' && r <= '9')
		if ok {
			b.WriteRune(r)
			dash = false
			continue
		}
		if !dash && b.Len() > 0 {
			b.WriteByte('-')
			dash = true
		}
	}
	out := strings.Trim(b.String(), "-")
	if len(out) > maxLen {
		out = strings.Trim(out[:maxLen], "-")
	}
	return out
}
