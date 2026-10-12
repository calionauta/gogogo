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
	"net/mail"
	"strconv"
	"strings"
	"sync"
	"time"

	"golang.org/x/crypto/argon2"

	"github.com/calionauta/gogogo/internal/db"
	dbgen "github.com/calionauta/gogogo/internal/db/sqlcgen"
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
	// totpAttempts throttles second-factor guessing (see ratelimit.go).
	totpAttempts attemptTracker
	// signinAttempts throttles password guessing per client IP (looser
	// budget than TOTP: signinAttemptMax). Wired in New; zero value would
	// inherit the TOTP budget, which is too tight for shared egress IPs.
	signinAttempts attemptTracker
	// totpKey seals TOTP secrets at rest (see totp_seal.go). Nil keeps the
	// legacy plaintext storage; set it via SetTOTPEncKey (BootChi wires
	// AUTH_ENC_KEY).
	totpKey *[32]byte
}

// New builds the auth stack on migrated queries. secureCookies is false in
// dev (plain HTTP) and true in production.
func New(q *dbgen.Queries, secureCookies bool) (*Auth, error) {
	if q == nil {
		return nil, errors.New("auth: nil queries")
	}
	return &Auth{
		q:              q,
		secure:         secureCookies,
		signinAttempts: attemptTracker{max: signinAttemptMax},
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
	if len(password) < minPasswordLen {
		return "", fmt.Errorf("auth: password too short (min %d chars)", minPasswordLen)
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

// verifyPassword compares in constant time under the hash's OWN version
// (not the current one), reporting whether it verified and whether the
// caller should rehash to the current version. Unknown formats and unknown
// versions fail closed (a v=2 hash from the future never verifies here,
// and a v=0 row never upgrades itself into validity).
func verifyPassword(encoded, password string) (ok, stale bool) {
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
	now := db.Now()
	row, err := a.q.CreateUser(ctx, dbgen.CreateUserParams{
		ID: db.NewID(), Email: addr, PasswordHash: hash, Name: name,
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
		TokenHash: hash, UserID: userID,
		Expires: now.Add(SessionTTL).Format(db.TimeFormat),
		Created: now.Format(db.TimeFormat),
	}); err != nil {
		return "", fmt.Errorf("auth: open session: %w", err)
	}
	_ = a.q.DeleteExpiredSessions(ctx, now.Format(db.TimeFormat))
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
			secReject(ctx, evLoginRejected, reasonUnknownAccount, addr, "")
			return "", false, ErrBadCredentials
		}
		return "", false, fmt.Errorf("auth: lookup: %w", err)
	}
	if ok, stale := verifyPassword(row.PasswordHash, password); !ok {
		secReject(ctx, evLoginRejected, reasonBadPassword, addr, row.ID)
		return "", false, ErrBadCredentials
	} else if stale {
		// Rotation without migration: the password verified under an old
		// version, so upgrade the row to the current one. Best-effort —
		// a bookkeeping write must not fail a verified login; the next
		// login retries.
		if fresh, herr := hashPassword(password); herr == nil {
			_ = a.q.UpdateUserPassword(ctx, dbgen.UpdateUserPasswordParams{
				PasswordHash: fresh, Updated: db.Now(), ID: row.ID,
			})
		}
	}
	if row.TotpSecret != "" && row.TotpVerified != 0 {
		return "", true, nil
	}
	token, err = a.openSession(ctx, row.ID)
	if err != nil {
		return "", false, err
	}
	return token, false, nil
}

// BeginTOTP starts enrollment for an authenticated user: stores a fresh
// secret (unverified) and returns it with the provisioning URL their
// authenticator app scans. Nothing is enforced until VerifyTOTPEnroll.
func (a *Auth) BeginTOTP(ctx context.Context, userID string) (secret, url, email string, err error) {
	user, err := a.q.GetUser(ctx, userID)
	if err != nil {
		return "", "", "", ErrBadCredentials
	}
	secret, err = newTOTPSecret()
	if err != nil {
		return "", "", "", err
	}
	// Seal before storing when a key is configured; the returned secret
	// stays plaintext (the user must scan it).
	stored := secret
	if a.totpKey != nil {
		stored, err = sealTOTPSecret(a.totpKey, secret)
		if err != nil {
			return "", "", "", err
		}
	}
	if err := a.q.SetTOTPSecret(ctx, dbgen.SetTOTPSecretParams{
		TotpSecret: stored, Updated: db.Now(), ID: userID,
	}); err != nil {
		return "", "", "", fmt.Errorf("auth: totp begin: %w", err)
	}
	return secret, totpProvisioningURL(user.Email, "gogogo", secret), user.Email, nil
}

// VerifyTOTPEnroll confirms the device (proving the user scanned the
// secret) and enforces 2FA from then on.
func (a *Auth) VerifyTOTPEnroll(ctx context.Context, userID, code string) error {
	user, err := a.q.GetUser(ctx, userID)
	if err != nil {
		return ErrBadCredentials
	}
	if user.TotpSecret == "" {
		return ErrBadCredentials
	}
	opened, sealed, err := openTOTPSecret(a.totpKey, user.TotpSecret)
	if err != nil || !verifyTOTP(opened, code, time.Now()) {
		return ErrBadCredentials
	}
	if err := a.q.VerifyTOTPFlag(ctx, dbgen.VerifyTOTPFlagParams{
		Updated: db.Now(), ID: userID,
	}); err != nil {
		return fmt.Errorf("auth: totp enroll: %w", err)
	}
	a.upgradeTOTPSecret(ctx, userID, opened, sealed)
	return nil
}

// VerifyTOTPLogin completes a password login pending second factor,
// opening the session the first step withheld. Accepts the current
// authenticator code or any unused recovery code (consumed on use).
func (a *Auth) VerifyTOTPLogin(ctx context.Context, userID, code string) (string, error) {
	user, err := a.q.GetUser(ctx, userID)
	if err != nil {
		return "", ErrBadCredentials
	}
	if user.TotpSecret == "" || user.TotpVerified == 0 {
		return "", ErrBadCredentials
	}
	if !a.verifySecondFactor(ctx, user, code) {
		secReject(ctx, evSecondFactorBad, reasonBadCode, user.Email, user.ID)
		return "", ErrBadCredentials
	}
	return a.openSession(ctx, user.ID)
}

// SetTOTPEncKey installs the at-rest encryption key for TOTP secrets
// (AUTH_ENC_KEY, 32 bytes). Empty clears back to legacy plaintext storage.
// Wrong-sized keys fail fast: a truncated key must never silently weaken
// every second factor.
func (a *Auth) SetTOTPEncKey(key []byte) error {
	if len(key) == 0 {
		a.totpKey = nil
		return nil
	}
	if len(key) != totpKeyLen {
		return fmt.Errorf("auth: totp key must be %d bytes, got %d", totpKeyLen, len(key))
	}
	var k [totpKeyLen]byte
	copy(k[:], key)
	a.totpKey = &k
	return nil
}

// upgradeTOTPSecret rewrites a legacy plaintext row sealed, preserving the
// verified flag (UpdateTOTPSecret, not SetTOTPSecret — the latter resets
// enrollment). No-op when already sealed or when no key is configured;
// best-effort, like password rehash: the verify already succeeded.
func (a *Auth) upgradeTOTPSecret(ctx context.Context, userID, opened string, sealed bool) {
	if sealed || a.totpKey == nil {
		return
	}
	resealed, err := sealTOTPSecret(a.totpKey, opened)
	if err != nil {
		return
	}
	_ = a.q.UpdateTOTPSecret(ctx, dbgen.UpdateTOTPSecretParams{
		TotpSecret: resealed, Updated: db.Now(), ID: userID,
	})
}

// DisableTOTP removes second-factor enforcement. Callers gate this behind
// a fresh password check (the HTTP route does).
func (a *Auth) DisableTOTP(ctx context.Context, userID string) error {
	if err := a.q.ClearTOTP(ctx, dbgen.ClearTOTPParams{Updated: db.Now(), ID: userID}); err != nil {
		return fmt.Errorf("auth: totp disable: %w", err)
	}
	return nil
}

// Logout revokes one session. Unknown tokens are a no-op (logout must be
// safe to repeat and must not reveal whether a token was valid).
func (a *Auth) Logout(ctx context.Context, token string) {
	if token == "" {
		return
	}
	_ = a.q.DeleteSession(ctx, tokenHash(token))
}
