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
	"encoding/json"
	"errors"
	"fmt"
	"net/http"
	"net/mail"
	"strings"
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
// a hot path. The encoded form carries no parameters, so tuning them
// later means rehash-on-next-login, not a migration.
const (
	argonTime    uint32 = 1
	argonMemory  uint32 = 64 * 1024
	argonThreads uint8  = 4
	argonKeyLen  uint32 = 32
	argonSaltLen        = 16
)

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
}

// New builds the auth stack on migrated queries. secureCookies is false in
// dev (plain HTTP) and true in production.
func New(q *dbgen.Queries, secureCookies bool) (*Auth, error) {
	if q == nil {
		return nil, errors.New("auth: nil queries")
	}
	return &Auth{q: q, secure: secureCookies}, nil
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

// hashPassword hashes with argon2id, encoded as argon2id$<salt>$<hash>
// (raw base64, no padding). Parameters live in code, so the string carries
// no version to parse and no downgrade path to exploit.
func hashPassword(password string) (string, error) {
	if len(password) < minPasswordLen {
		return "", fmt.Errorf("auth: password too short (min %d chars)", minPasswordLen)
	}
	salt := make([]byte, argonSaltLen)
	if _, err := rand.Read(salt); err != nil {
		return "", fmt.Errorf("auth: salt: %w", err)
	}
	hash := argon2.IDKey([]byte(password), salt, argonTime, argonMemory, argonThreads, argonKeyLen)
	return "argon2id$" + base64.RawStdEncoding.EncodeToString(salt) +
		"$" + base64.RawStdEncoding.EncodeToString(hash), nil
}

// verifyPassword compares in constant time. Unknown formats fail closed.
func verifyPassword(encoded, password string) bool {
	parts := strings.Split(encoded, "$")
	if len(parts) != 3 || parts[0] != "argon2id" {
		return false
	}
	salt, err1 := base64.RawStdEncoding.DecodeString(parts[1])
	want, err2 := base64.RawStdEncoding.DecodeString(parts[2])
	if err1 != nil || err2 != nil {
		return false
	}
	got := argon2.IDKey([]byte(password), salt, argonTime, argonMemory, argonThreads, argonKeyLen)
	return subtle.ConstantTimeCompare(got, want) == 1
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

// Login verifies the password and opens a session, returning the raw
// cookie value. Unknown email and wrong password are indistinguishable.
// Expired sessions are pruned on the way in so the table cannot grow
// without bound on an instance nobody ever logs out of.
func (a *Auth) Login(ctx context.Context, email, password string) (token string, err error) {
	addr, err := normalizeEmail(email)
	if err != nil {
		return "", ErrBadCredentials
	}
	row, err := a.q.GetUserByEmail(ctx, addr)
	if err != nil {
		if errors.Is(err, sql.ErrNoRows) {
			// Same cost shape as a real check would be nicer, but a fixed
			// dummy hash keeps timing differences below noise without
			// pretending to be a side-channel proof.
			_, _ = hashPassword("dummy-placeholder-00")
			return "", ErrBadCredentials
		}
		return "", fmt.Errorf("auth: lookup: %w", err)
	}
	if !verifyPassword(row.PasswordHash, password) {
		return "", ErrBadCredentials
	}
	return a.openSession(ctx, row.ID)
}

// Logout revokes one session. Unknown tokens are a no-op (logout must be
// safe to repeat and must not reveal whether a token was valid).
func (a *Auth) Logout(ctx context.Context, token string) {
	if token == "" {
		return
	}
	_ = a.q.DeleteSession(ctx, tokenHash(token))
}

// Session validates the request's session cookie. Expired or unknown
// sessions fail closed; callers redirect to /login.
func (a *Auth) Session(r *http.Request) (*ValidatedSession, error) {
	cookie, err := r.Cookie(CookieName)
	if err != nil || cookie.Value == "" {
		return nil, errors.New("auth: no session cookie")
	}
	row, err := a.q.GetSession(r.Context(), tokenHash(cookie.Value))
	if err != nil {
		return nil, errors.New("auth: unknown session")
	}
	expires, err := time.Parse(db.TimeFormat, row.Expires)
	if err != nil {
		return nil, fmt.Errorf("auth: session expiry unreadable: %w", err)
	}
	if time.Now().UTC().After(expires) {
		_ = a.q.DeleteSession(r.Context(), row.TokenHash)
		return nil, errors.New("auth: session expired")
	}
	user, err := a.q.GetUser(r.Context(), row.UserID)
	if err != nil {
		return nil, errors.New("auth: session owner gone")
	}
	return &ValidatedSession{User: &User{ID: user.ID, Email: user.Email}, Expires: expires}, nil
}

// sessionCookie builds the cookie for a fresh login.
func (a *Auth) sessionCookie(token string) *http.Cookie {
	// #nosec G124 — Secure is false in dev (plain-HTTP localhost) by explicit
	// config; production boots with secureCookies and the real flag.
	return &http.Cookie{
		Name:     CookieName,
		Value:    token,
		Path:     "/",
		MaxAge:   int(SessionTTL.Seconds()),
		HttpOnly: true,
		Secure:   a.secure,
		SameSite: http.SameSiteLaxMode,
	}
}

// clearCookie expires the session cookie (logout + invalid-token cleanup).
func (a *Auth) clearCookie(w http.ResponseWriter) {
	// #nosec G124 — same deliberate dev/prod split as sessionCookie above.
	http.SetCookie(w, &http.Cookie{
		Name:     CookieName,
		Value:    "",
		Path:     "/",
		MaxAge:   -1,
		HttpOnly: true,
		Secure:   a.secure,
		SameSite: http.SameSiteLaxMode,
	})
}

// ctxKey carries the validated session. Private so only SessionFrom reads it.
type ctxKey struct{}

// RequireSession is chi middleware: requests without a valid session get
// 401 with the cookie cleared, the rest reach the handler with the session
// in context. Data endpoints that prefer a login redirect (303) resolve
// the session themselves via Session instead.
func (a *Auth) RequireSession(next http.Handler) http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		sess, err := a.Session(r)
		if err != nil {
			a.clearCookie(w)
			http.Error(w, "unauthorized", http.StatusUnauthorized)
			return
		}
		next.ServeHTTP(w, r.WithContext(context.WithValue(r.Context(), ctxKey{}, sess)))
	})
}

// SessionFrom returns the session RequireSession stored in the context.
func SessionFrom(ctx context.Context) (*ValidatedSession, error) {
	sess, ok := ctx.Value(ctxKey{}).(*ValidatedSession)
	if !ok || sess == nil {
		return nil, errors.New("auth: no session in context")
	}
	return sess, nil
}

// Handler serves the auth API under /api/auth (mount at /): signup,
// signin and signout with the same paths and JSON shapes the limen stack
// used, so clients cut over without noticing the backend changed.
func (a *Auth) Handler() http.Handler {
	mux := http.NewServeMux()
	mux.HandleFunc("/api/auth/signup/credential", a.handleSignup)
	mux.HandleFunc("/api/auth/signin/credential", a.handleSignin)
	mux.HandleFunc("/api/auth/signout", a.handleSignout)
	return mux
}

func readJSON(r *http.Request, dst any) error {
	defer r.Body.Close()
	dec := json.NewDecoder(r.Body)
	dec.DisallowUnknownFields()
	return dec.Decode(dst)
}

func writeJSON(w http.ResponseWriter, code int, v any) {
	w.Header().Set("Content-Type", "application/json")
	w.WriteHeader(code)
	_ = json.NewEncoder(w).Encode(v)
}

func (a *Auth) handleSignup(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodPost {
		http.Error(w, "method not allowed", http.StatusMethodNotAllowed)
		return
	}
	var body struct {
		Email    string `json:"email"`
		Password string `json:"password"`
		Name     string `json:"name"`
	}
	if err := readJSON(r, &body); err != nil {
		http.Error(w, "invalid request", http.StatusBadRequest)
		return
	}
	user, token, err := a.Signup(r.Context(), body.Email, body.Password, body.Name)
	if err != nil {
		if errors.Is(err, ErrBadCredentials) {
			http.Error(w, "invalid credentials", http.StatusUnauthorized)
			return
		}
		http.Error(w, "invalid request", http.StatusBadRequest)
		return
	}
	http.SetCookie(w, a.sessionCookie(token))
	writeJSON(w, http.StatusOK, map[string]string{"email": user.Email})
}

func (a *Auth) handleSignin(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodPost {
		http.Error(w, "method not allowed", http.StatusMethodNotAllowed)
		return
	}
	var body struct {
		Credential string `json:"credential"`
		Password   string `json:"password"`
	}
	if err := readJSON(r, &body); err != nil {
		http.Error(w, "invalid request", http.StatusBadRequest)
		return
	}
	token, err := a.Login(r.Context(), body.Credential, body.Password)
	if err != nil {
		if errors.Is(err, ErrBadCredentials) {
			http.Error(w, "invalid credentials", http.StatusUnauthorized)
			return
		}
		http.Error(w, "login failed", http.StatusInternalServerError)
		return
	}
	http.SetCookie(w, a.sessionCookie(token))
	writeJSON(w, http.StatusOK, map[string]string{"status": "ok"})
}

func (a *Auth) handleSignout(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodPost {
		http.Error(w, "method not allowed", http.StatusMethodNotAllowed)
		return
	}
	if cookie, err := r.Cookie(CookieName); err == nil {
		a.Logout(r.Context(), cookie.Value)
	}
	a.clearCookie(w)
	writeJSON(w, http.StatusOK, map[string]string{"status": "ok"})
}
