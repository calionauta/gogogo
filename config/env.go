// SCOPE:core — Environment readers shared by the config loaders.
package config

import (
	"os"
	"strconv"
	"time"
)

func getEnv(key, fallback string) string {
	if v := os.Getenv(key); v != "" {
		return v
	}
	return fallback
}

// envBool reads a boolean env var, falling back to def when unset or
// unparseable. The fallback lets a config file or env var (e.g.
// NATS_ENABLED=false) override the default at runtime; every feature is
// always compiled, so there is no build tag to flip.
func envBool(key string, def bool) bool {
	v := os.Getenv(key)
	if v == "" {
		return def
	}
	b, err := strconv.ParseBool(v)
	if err != nil {
		return def
	}
	return b
}

// envInt reads an integer env var, falling back to def when unset or
// unparseable.
func envInt(key string, def int) int {
	v := os.Getenv(key)
	if v == "" {
		return def
	}
	n, err := strconv.Atoi(v)
	if err != nil {
		return def
	}
	return n
}

// envDuration reads a duration env var ("250ms", "2s"), falling back to def
// when unset or unparseable. A non-positive override is treated as unset:
// zero would silently delete intentional pacing rather than tune it.
func envDuration(key string, def time.Duration) time.Duration {
	v := os.Getenv(key)
	if v == "" {
		return def
	}
	d, err := time.ParseDuration(v)
	if err != nil || d <= 0 {
		return def
	}
	return d
}

// defaultDagNatsNATSPort is the conventional NATS port the DagNats engine
// owns. The realtime broadcaster connects here (single-NATS convention).
const defaultDagNatsNATSPort = 4222

// defaultGreetPacing is the onboarding-greet pause. It exists purely so a human
// watching the stepper can read the "greeting" phase; see DagNats.GreetPacing.
// Long enough to be perceptible, short enough not to feel sluggish.
const defaultGreetPacing = 1500 * time.Millisecond

// defaultAppName falls back to the binary name when APP_NAME is unset
// so the secrets file scope tracks whatever the project owner actually
// compiled. Uses os.Args[0] (binary path) trimmed to base name; if that
// fails (e.g. tests), it returns a hard-coded stable name.
func defaultAppName() string {
	exe, err := os.Executable()
	if err != nil || exe == "" {
		return "gogogo"
	}
	base := exe
	for i := len(exe) - 1; i >= 0; i-- {
		if exe[i] == '/' {
			base = exe[i+1:]
			break
		}
	}
	if base == "" {
		return "gogogo"
	}
	return base
}
