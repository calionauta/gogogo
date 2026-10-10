// SCOPE:core
package secrets

import (
	"log/slog"
	"strings"
	"testing"
)

// TestLoadConfiguredButUnreadable pins the fail-loud contract: when
// AGE_SECRET_KEY is set but the secrets file cannot be read, Load must
// log an Error naming the project — silently booting keyless (the old
// behavior) turns a broken deploy into a mystery of hidden buttons.
func TestLoadConfiguredButUnreadable(t *testing.T) {
	// Not parallel: mutates process env + the default logger.
	t.Setenv("AGE_SECRET_KEY", "AGE-SECRET-KEY-1AAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAA")
	var buf strings.Builder
	prev := slog.Default()
	slog.SetDefault(slog.New(slog.NewTextHandler(&buf, nil)))
	defer slog.SetDefault(prev)

	loadFrom(t.TempDir(), "nosuchproj", "AGE-SECRET-KEY-1AAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAA")
	if out := buf.String(); !strings.Contains(out, "nosuchproj") || !strings.Contains(out, "ERROR") {
		t.Fatalf("missing-file load logged nothing actionable, got:\n%s", out)
	}
}
