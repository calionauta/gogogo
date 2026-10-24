// SCOPE:core — Auth on the flat stack (argon2id + sessions table)
//
// Package auth owns password identity with zero dependencies beyond the
// standard library, x/crypto and database/sql: argon2id password hashes,
// opaque session tokens (only their SHA-256 reaches the database), and the
// gogogo_auth cookie. Sessions live in the sessions table, identities in
// users. Handlers never touch SQL here — they go through Signup, Login,
// Logout and Session, so a wrong call is a compile error and every auth
// decision stays in one file an LLM can hold in its head.
//
// Magic link, OTP and social logins are deliberately OUT: each needs
// external infrastructure (an email sender, TOTP enrollment, OAuth client
// IDs) that no library eliminates, and the template core does not need
// them. They arrive as per-app plugins; password + session is the part
// every app needs, so it is owned, small, and stable.
package auth

import (
	"context"
	"crypto/rand"
	"crypto/sha256"
	"crypto/subtle"
	"database/sql"
	"encoding/base64"
	"encoding/hex"
	"errors"
	"fmt"
	"net/http"
	"net/mail"
	"strconv"
	"strings"
	"sync"
	"time"

	"golang.org/x/crypto/argon2"

	dbgen "github.com/calionauta/gogogo/internal/db/sqlcgen"
	"github.com/calionauta/gogogo/internal/dbschema"
)

// CookieName is the app's session cookie. Kept distinct from any provider
// cookie on purpose: one cookie, one session, no clobbering.
const CookieName = "gogogo_auth"

// SessionTTL is how long a login lasts before re-authentication.
const SessionTTL = 7 * 24 * time.Hour

// Argon2id cost parameters. Login/signup pay one hash each (a fraction of
// a second on modern hardware) — fine for an interactive flow, never for
// a hot path.
//
// Versions are a registry, not a constant: rotation means adding a new
// entry and pointing argonVersion at it. Old hashes keep verifying under
// their own entry, and Login rehashes them to the current version on the
// next successful login (rehash-on-login) — no migration, no lockout.
// The encoded form carries its version ("argon2id$v=<n>$..."), unknown
// versions fail closed in verifyPassword.
const (
	// Calibrated 2026-10 (M4, ~250ms): above the OWASP floor (19 MiB,
	// t=2, p=1) with headroom for slower hardware. Review annually
	// against the OWASP sheet.
	argonSaltLen = 16
)

// argonVersion is the version new hashes are written under. Rotation is:
// add an entry to argonVersions, point this at it, done — old rows verify
// under their own entry and upgrade themselves at the next login
// (TestPasswordRotation pins both directions). A var (not const) so tests
// can stage a rotation without waiting for a real one.
var argonVersion uint32 = 1

// argonVersions certifies one encoded version each.
var argonVersions = map[uint32]argonParams{
	1: {time: 2, memory: 128 * 1024, threads: 1, keyLen: 32},
}

// argonParams is one version's cost parameters.
type argonParams struct {
	time    uint32
	memory  uint32
	threads uint8
	keyLen  uint32
}

// minPasswordLen is the signup floor. Short secrets fall to offline
// brute force regardless of hashing cost, so length is enforced first.
const minPasswordLen = 8

// tokenLen is the session entropy in bytes (256 bits). The cookie carries
// base64url of this; only its SHA-256 is stored.
const tokenLen = 32

// ErrBadCredentials is returned by Login when the email is unknown or the
// password is wrong, and by Signup on a duplicate email. One sentinel for
// all three so no caller can enumerate accounts.
var ErrBadCredentials = errors.New("auth: bad credentials")

// User is the authenticated identity. ID is a UUIDv7 string, matching the
// users table and every owner column in the schema.
type User struct {
	ID    string
	Email string
}

// ValidatedSession is the result of validating a request's session cookie.
type ValidatedSession struct {
	User    *User
	Expires time.Time
	// SessionID is the stable public id of THIS session (the cookie token is
	// never exposed). The account page compares it to the listed rows to mark
	// the current device.
	SessionID string
	// Role is the coarse RBAC subject ('admin', 'viewer', or 'user',
	// from users.role). The middleware reads it for enforcement; handlers
	// may read it for display hints only (disabled affordances with a
	// message) — never for access decisions, which stay in policy.csv.
	// A wrong hint degrades to a 403; a wrong decision would be a write.
	Role string
}

// Auth is the flat auth stack. Build it once at boot and share it; all
// methods are safe for concurrent use. No secrets, no config structs —
// sessions are random tokens, so there is nothing to rotate.
type Auth struct {
	q      *dbgen.Queries
	secure bool

	mu     sync.Mutex
	oauth  map[string]OAuthProvider
	states map[string]oauthState
	sender MagicSender
	// appURL is the configured public origin for out-of-band links. Empty
	// disables them: the request Host is never trusted to build one.
	appURL string
	// signupDisabled closes public account creation (password signup and
	// first-login OAuth provisioning). Zero value keeps the template's
	// historical behaviour for package-level tests; BootChi always sets it
	// from config, where production defaults to closed.
	signupDisabled bool
	// magicBindBrowser forces a magic link to be completed in the requesting
	// browser. Off by default; cross-device is the expected UX.
	magicBindBrowser bool
	// magicConfirm renders the read-only email-link landing. Nil falls back
	// to completing directly (package tests without the server UI).
	magicConfirm func(w http.ResponseWriter, r *http.Request, email, token, bind, errMsg string)
	// passwordResetPage renders the set-a-new-password form.
	passwordResetPage func(w http.ResponseWriter, r *http.Request, email, token, errMsg string)
	// mailer delivers verification/reset messages. Nil disables those flows.
	mailer Mailer
	// requireEmailVerification blocks password login until the address is
	// verified. Off by default; OAuth-verified addresses bypass it.
	requireEmailVerification bool
	// sessionIdle is the inactivity window; zero keeps the absolute TTL only.
	sessionIdle time.Duration
	// totpAttempts throttles second-factor guessing (see ratelimit.go).
	totpAttempts attemptTracker
	// signinAttempts throttles password guessing per client IP (looser
	// budget than TOTP: signinAttemptMax). Wired in New; zero value would
	// inherit the TOTP budget, which is too tight for shared egress IPs.
	signinAttempts attemptTracker
	// signupAttempts bounds account-creation work per client IP. Each
	// signup burns one argon2id hash (128 MiB), so an unthrottled public
	// signup is a memory-exhaustion lever even when the account is valid.
	signupAttempts attemptTracker
	// magicAttempts bounds magic-link requests per client IP: the per-account
	// cooldown stops one mailbox being spammed, not a list of mailboxes.
	magicAttempts attemptTracker
	// oauthStartAttempts bounds OAuth handshake starts per client IP; each
	// one holds an in-process state entry for ten minutes.
	oauthStartAttempts attemptTracker
	// resetAttempts bounds password-reset mail requests per client IP.
	resetAttempts attemptTracker
	// verifyAttempts bounds verification-mail requests per client IP.
	verifyAttempts attemptTracker
	// totpKey seals TOTP secrets at rest (see totp_seal.go). Nil disables
	// TOTP enrollment and verification; set it via SetTOTPEncKey (BootChi
	// wires AUTH_ENC_KEY or the auto-created key file).
	totpKey *[32]byte
	// sessionRecheck overrides how often an open session stream re-validates
	// the cookie (see session_stream.go). Zero uses defaultSessionRecheck;
	// tests set it directly to exercise expiry without waiting a minute.
	sessionRecheck time.Duration
}

// New builds the auth stack on migrated queries. secureCookies is false in
// dev (plain HTTP) and true in production.
func New(q *dbgen.Queries, secureCookies bool) (*Auth, error) {
	if q == nil {
		return nil, errors.New("auth: nil queries")
	}
	return &Auth{
		q:                  q,
		secure:             secureCookies,
		signinAttempts:     attemptTracker{max: signinAttemptMax},
		signupAttempts:     attemptTracker{max: signupAttemptMax},
		magicAttempts:      attemptTracker{max: magicAttemptMax},
		oauthStartAttempts: attemptTracker{max: oauthStartAttemptMax},
		resetAttempts:      attemptTracker{max: resetAttemptMax},
		verifyAttempts:     attemptTracker{max: resetAttemptMax},
	}, nil
}

// normalizeEmail lowercases and validates the address. The users table
// stores lowercased emails under a plain unique index, so normalization
// here (not in SQL) is what makes Test@x and test@x the same account.
func normalizeEmail(email string) (string, error) {
	email = strings.ToLower(strings.TrimSpace(email))
	if email == "" {
		return "", errors.New("auth: empty email")
	}
	if _, err := mail.ParseAddress(email); err != nil {
		return "", fmt.Errorf("auth: invalid email: %w", err)
	}
	return email, nil
}

// hashPassword hashes with argon2id, encoded as argon2id$v=<n>$<salt>$<hash>
// (raw base64, no padding) under the CURRENT version.
func hashPassword(password string) (string, error) {
	if err := validatePassword(password); err != nil {
		return "", err
	}
	salt := make([]byte, argonSaltLen)
	if _, err := rand.Read(salt); err != nil {
		return "", fmt.Errorf("auth: salt: %w", err)
	}
	p, ok := argonVersions[argonVersion]
	if !ok {
		return "", fmt.Errorf("auth: unknown argon version %d", argonVersion)
	}
	hash := argon2.IDKey([]byte(password), salt, p.time, p.memory, p.threads, p.keyLen)
	return "argon2id$v=" + strconv.FormatUint(uint64(argonVersion), 10) +
		"$" + base64.RawStdEncoding.EncodeToString(salt) +
		"$" + base64.RawStdEncoding.EncodeToString(hash), nil
}

// validatePassword applies the length policy without paying argon2. The
// password-change handler uses it before revoking sessions, so an invalid
// new password does not log the user out for nothing.
func validatePassword(password string) error {
	if len(password) < minPasswordLen {
		return fmt.Errorf("auth: password too short (min %d chars)", minPasswordLen)
	}
	if len(password) > maxPasswordLen {
		return ErrPasswordTooLong
	}
	return nil
}

// verifyPassword compares in constant time under the hash's OWN version
// (not the current one), reporting whether it verified and whether the
// caller should rehash to the current version. Unknown formats and unknown
// versions fail closed (a v=2 hash from the future never verifies here,
// and a v=0 row never upgrades itself into validity).
func verifyPassword(encoded, password string) (ok, stale bool) {
	if len(password) > maxPasswordLen {
		return false, false
	}
	parts := strings.Split(encoded, "$")
	if len(parts) != 4 || parts[0] != "argon2id" || !strings.HasPrefix(parts[1], "v=") {
		return false, false
	}
	n, err := strconv.ParseUint(strings.TrimPrefix(parts[1], "v="), 10, 32)
	if err != nil {
		return false, false
	}
	p, known := argonVersions[uint32(n)]
	if !known {
		return false, false
	}
	salt, err1 := base64.RawStdEncoding.DecodeString(parts[2])
	want, err2 := base64.RawStdEncoding.DecodeString(parts[3])
	if err1 != nil || err2 != nil {
		return false, false
	}
	got := argon2.IDKey([]byte(password), salt, p.time, p.memory, p.threads, p.keyLen)
	if subtle.ConstantTimeCompare(got, want) != 1 {
		return false, false
	}
	return true, uint32(n) != argonVersion
}

// newToken returns a random session token (cookie value) and its storage
// hash. Only the hash reaches the database: a leaked copy cannot be
// replayed as a login.
func newToken() (token, hash string, err error) {
	raw := make([]byte, tokenLen)
	if _, err := rand.Read(raw); err != nil {
		return "", "", fmt.Errorf("auth: token entropy: %w", err)
	}
	token = base64.RawURLEncoding.EncodeToString(raw)
	return token, tokenHash(token), nil
}

// tokenHash maps a presented cookie value to its storage hash.
func tokenHash(token string) string {
	sum := sha256.Sum256([]byte(token))
	return hex.EncodeToString(sum[:])
}

// Signup creates the identity and opens its first session (auto sign-in,
// matching the previous stack's behaviour). A duplicate email fails with
// ErrBadCredentials — the same sentinel as a wrong password.
func (a *Auth) Signup(ctx context.Context, email, password, name string) (*User, string, error) {
	addr, err := normalizeEmail(email)
	if err != nil {
		return nil, "", err
	}
	hash, err := hashPassword(password)
	if err != nil {
		return nil, "", err
	}
	now := dbschema.Now()
	row, err := a.q.CreateUser(ctx, dbgen.CreateUserParams{
		ID: dbschema.NewID(), Email: addr, PasswordHash: hash, Name: name,
		Created: now, Updated: now,
	})
	if err != nil {
		// UNIQUE(email) is the only expected conflict: same sentinel.
		return nil, "", ErrBadCredentials
	}
	token, err := a.openSession(ctx, row.ID)
	if err != nil {
		return nil, "", err
	}
	a.secEvent(ctx, evSignupSucceeded, row.ID)
	return &User{ID: row.ID, Email: row.Email}, token, nil
}

// openSession creates a session row and returns the raw cookie value.
func (a *Auth) openSession(ctx context.Context, userID string) (string, error) {
	token, hash, err := newToken()
	if err != nil {
		return "", err
	}
	now := time.Now().UTC()
	if err := a.q.CreateSession(ctx, dbgen.CreateSessionParams{
		TokenHash: hash, UserID: userID, SessionID: dbschema.NewID(),
		LastSeen: now.Format(dbschema.TimeFormat),
		Expires:  now.Add(SessionTTL).Format(dbschema.TimeFormat),
		Created:  now.Format(dbschema.TimeFormat),
	}); err != nil {
		return "", fmt.Errorf("auth: open session: %w", err)
	}
	_ = a.q.DeleteExpiredSessions(ctx, now.Format(dbschema.TimeFormat))
	return token, nil
}

// Login verifies the password. It returns either a session token or
// totpRequired=true when the account has verified 2FA — in that case no
// session opens until VerifyTOTPLogin succeeds with the user's id and a
// current code. Unknown email and wrong password are indistinguishable.
// Expired sessions are pruned on the way in so the table cannot grow
// without bound on an instance nobody ever logs out of.
func (a *Auth) Login(ctx context.Context, email, password string) (token string, totpRequired bool, err error) {
	addr, err := normalizeEmail(email)
	if err != nil {
		return "", false, ErrBadCredentials
	}
	row, err := a.q.GetUserByEmail(ctx, addr)
	if err != nil {
		if errors.Is(err, sql.ErrNoRows) {
			// Same cost shape as a real check would be nicer, but a fixed
			// dummy hash keeps timing differences below noise without
			// pretending to be a side-channel proof.
			_, _ = hashPassword("dummy-placeholder-00")
			a.secReject(ctx, evLoginRejected, reasonUnknownAccount, addr, "")
			return "", false, ErrBadCredentials
		}
		return "", false, fmt.Errorf("auth: lookup: %w", err)
	}
	if ok, stale := verifyPassword(row.PasswordHash, password); !ok {
		a.secReject(ctx, evLoginRejected, reasonBadPassword, addr, row.ID)
		return "", false, ErrBadCredentials
	} else if stale {
		// Rotation without migration: the password verified under an old
		// version, so upgrade the row to the current one. Best-effort —
		// a bookkeeping write must not fail a verified login; the next
		// login retries.
		if fresh, herr := hashPassword(password); herr == nil {
			_ = a.q.UpdateUserPassword(ctx, dbgen.UpdateUserPasswordParams{
				PasswordHash: fresh, Updated: dbschema.Now(), ID: row.ID,
			})
		}
	}
	if a.emailVerificationRequired() && row.EmailVerifiedAt == "" {
		a.secReject(ctx, evLoginRejected, reasonEmailUnverified, addr, row.ID)
		return "", false, ErrEmailUnverified
	}
	if row.TotpSecret != "" && row.TotpVerified != 0 {
		return "", true, nil
	}
	token, err = a.openSession(ctx, row.ID)
	if err != nil {
		return "", false, err
	}
	a.secEvent(ctx, evLoginSucceeded, row.ID)
	return token, false, nil
}

// Logout revokes one session. Unknown tokens are a no-op (logout must be
// safe to repeat and must not reveal whether a token was valid).
func (a *Auth) Logout(ctx context.Context, token string) {
	if token == "" {
		return
	}
	_ = a.q.DeleteSession(ctx, tokenHash(token))
}
