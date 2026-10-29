// SCOPE:core — test helper: a TOTP code that is provably not valid right now.
package auth

import (
	"testing"
	"time"
)

// wrongTOTPCode returns a well-formed six-digit code that is NOT accepted for
// secret right now.
//
// The tests used a hardcoded "000000" as the wrong code, and that is a valid
// code about once in 600,000 window samples (measured over 200k random secrets
// against the three windows verifyTOTP accepts — it allows one step of clock
// skew either way). Rare is not the same as impossible: this is exactly what
// failed CI run 40877204282, as
//
//	stepup_test.go:71: wrong code accepted: ok=true err=<nil>
//
// on a security assertion whose whole point is that the wrong code is refused.
// The code is taken from a window ten steps away and compared against the three
// accepted ones, so it cannot collide by construction rather than by luck.
func wrongTOTPCode(t *testing.T, secret string) string {
	t.Helper()
	now := time.Now()
	valid := make(map[string]bool, 3)
	for _, skew := range []int64{-1, 0, 1} {
		c, err := totpCode(secret, now.Add(time.Duration(skew)*totpStep))
		if err != nil {
			t.Fatalf("totp: %v", err)
		}
		valid[c] = true
	}
	for i := int64(10); i < 20; i++ {
		c, err := totpCode(secret, now.Add(time.Duration(i)*totpStep))
		if err != nil {
			t.Fatalf("totp: %v", err)
		}
		if !valid[c] {
			return c
		}
	}
	t.Fatal("no non-colliding TOTP code found in ten far windows")
	return ""
}

// TestWrongTOTPCodeIsNeverAccepted pins the helper's contract so a later
// "simplification" back to a fixed value fails here instead of on a security
// assertion at 3am. 200 fresh secrets is far past the 1-in-600,000 collision
// rate the fixed value had.
func TestWrongTOTPCodeIsNeverAccepted(t *testing.T) {
	for range 200 {
		secret, err := newTOTPSecret()
		if err != nil {
			t.Fatalf("secret: %v", err)
		}
		if verifyTOTP(secret, wrongTOTPCode(t, secret), time.Now()) {
			t.Fatal("wrongTOTPCode returned a code verifyTOTP accepts")
		}
	}
}
