// SCOPE:core — security-event logging contract tests.
//
// A security log that nobody asserts on rots quietly: a refactor drops the
// call and nothing fails. These tests pin both directions — the event fires
// with its reason, and the secret it must never carry is absent from the
// output.
package auth

import (
	"bytes"
	"context"
	"log/slog"
	"strings"
	"testing"
)

// captureSecLogs redirects the default slog logger into a buffer for the
// duration of one test and returns it. Restored on cleanup, so a parallel
// package test cannot observe another test's records.
func captureSecLogs(t *testing.T) *bytes.Buffer {
	t.Helper()
	var buf bytes.Buffer
	prev := slog.Default()
	slog.SetDefault(slog.New(slog.NewJSONHandler(&buf, &slog.HandlerOptions{Level: slog.LevelInfo})))
	t.Cleanup(func() { slog.SetDefault(prev) })
	return &buf
}

// secFixture builds the auth stack over a migrated temp database, using the
// same helpers the rest of the package's tests use.
func secFixture(t *testing.T) *Auth {
	t.Helper()
	return testAuth(t, testDB(t))
}

func TestSecRejectLogsEventAndReason(t *testing.T) {
	buf := captureSecLogs(t)
	secReject(context.Background(), evLoginRejected, reasonBadPassword, "a@example.com", "user1")

	out := buf.String()
	for _, want := range []string{
		`"level":"WARN"`,
		`"msg":"auth: login rejected"`,
		`"event":"login rejected"`,
		`"reason":"bad password"`,
		`"email":"a@example.com"`,
		`"userID":"user1"`,
	} {
		if !strings.Contains(out, want) {
			t.Fatalf("missing %s in security log:\n%s", want, out)
		}
	}
}

func TestSecRejectCarriesNoSecret(t *testing.T) {
	buf := captureSecLogs(t)
	const password = "Correct-horse-99!"
	secReject(context.Background(), evLoginRejected, reasonBadPassword, "a@example.com", "user1")

	out := buf.String()
	if strings.Contains(out, password) {
		t.Fatalf("security log leaked the password:\n%s", out)
	}
	// The shape of the secret must not be loggable either: these tests pass
	// no token or code, so their absence is structural, not incidental.
	for _, forbidden := range []string{"token", "code", "secret", "hash"} {
		if strings.Contains(out, `"`+forbidden+`"`) {
			t.Fatalf("security log carries a %q key, which must never be logged:\n%s", forbidden, out)
		}
	}
}

func TestSecEventLogsAtInfo(t *testing.T) {
	buf := captureSecLogs(t)
	secEvent(context.Background(), evMagicConsumed, "user2")

	out := buf.String()
	if !strings.Contains(out, `"level":"INFO"`) {
		t.Fatalf("lifecycle event must log at INFO:\n%s", out)
	}
	if !strings.Contains(out, `"event":"magic link consumed"`) {
		t.Fatalf("missing event name:\n%s", out)
	}
}

// TestLoginRejectedIsLogged proves the service layer, not just the helper,
// emits the event — the call site is the part a refactor can drop.
func TestLoginRejectedIsLogged(t *testing.T) {
	buf := captureSecLogs(t)
	a := secFixture(t)
	ctx := context.Background()

	if _, _, err := a.Login(ctx, "nobody@example.com", "Whatever-99"); err == nil {
		t.Fatal("login for an unknown account must fail")
	}
	if out := buf.String(); !strings.Contains(out, `"reason":"unknown account"`) {
		t.Fatalf("unknown-account login was not logged:\n%s", out)
	}

	_, _, err := a.Signup(ctx, "known@example.com", "Correct-horse-99!", "Known")
	if err != nil {
		t.Fatalf("signup: %v", err)
	}
	buf.Reset()
	if _, _, err := a.Login(ctx, "known@example.com", "Wrong-password-1"); err == nil {
		t.Fatal("login with a bad password must fail")
	}
	out := buf.String()
	for _, want := range []string{`"reason":"bad password"`, `"userID":`} {
		if !strings.Contains(out, want) {
			t.Fatalf("bad-password login missing %s:\n%s", want, out)
		}
	}
	// The password itself is the one thing that must not appear.
	if strings.Contains(out, "Wrong-password-1") {
		t.Fatalf("security log leaked the submitted password:\n%s", out)
	}
}

// TestMagicLinkRejectedIsLogged covers both rejection reasons on the one path
// that deliberately produces indistinguishable client-facing errors.
func TestMagicLinkRejectedIsLogged(t *testing.T) {
	buf := captureSecLogs(t)
	a := secFixture(t)
	ctx := context.Background()

	if _, err := a.VerifyMagicLink(ctx, "not-a-real-token"); err == nil {
		t.Fatal("unknown magic token must fail")
	}
	if out := buf.String(); !strings.Contains(out, `"reason":"unknown token"`) {
		t.Fatalf("unknown magic token was not logged:\n%s", out)
	}
}
