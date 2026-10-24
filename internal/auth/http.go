// SCOPE:core — Flat auth HTTP surface (signup/signin/signout)
package auth

import (
	"encoding/json"
	"errors"
	"net/http"

	"github.com/go-chi/chi/v5"
)

// noStore keeps auth bodies out of every cache: they carry session cookies
// and one-time codes. It lives on the auth router so the policy travels with
// the surface instead of a path prefix in the edge.
func noStore(next http.Handler) http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Cache-Control", "no-store")
		next.ServeHTTP(w, r)
	})
}

// captureUserAgent puts the request's User-Agent on the context openSession
// reads, so every auth entry point (signup, password, magic, OAuth) records
// the same hint without growing each method signature.
func captureUserAgent(next http.Handler) http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		next.ServeHTTP(w, r.WithContext(WithUserAgent(r.Context(), r.UserAgent())))
	})
}

// Routes returns the auth API router: signup, signin and signout, plus the
// magic-link, TOTP second-factor and OAuth entry points. Every pattern is
// absolute, so the composition root flattens it with MountFeature like any
// feature — the router owns the method decision (405) and the provider
// parameter, and the route table sees each endpoint.
func (a *Auth) Routes() chi.Router {
	r := chi.NewRouter()
	r.Use(noStore, captureUserAgent)
	r.Post("/api/auth/signup/credential", a.handleSignup)
	r.Post("/api/auth/signin/credential", a.handleSignin)
	r.Post("/api/auth/signout", a.handleSignout)
	r.Get("/api/auth/sessions", a.handleListSessions)
	r.Delete("/api/auth/sessions/{id}", a.handleRevokeSession)
	r.Post("/api/auth/sessions/revoke-all", a.handleRevokeAll)
	r.Post("/api/auth/password", a.handlePasswordChange)
	r.Post("/api/auth/verify/request", a.handleVerifyRequest)
	r.Get("/api/auth/verify", a.handleVerifyEmail)
	r.Post("/api/auth/password/reset/request", a.handlePasswordResetRequest)
	r.Get("/api/auth/password/reset", a.handlePasswordResetForm)
	r.Post("/api/auth/password/reset", a.handlePasswordResetComplete)
	r.Post("/api/auth/magic/request", a.handleMagicRequest)
	r.Get("/api/auth/magic", a.handleMagicVerify)
	r.Post("/api/auth/magic", a.handleMagicComplete)
	r.Post("/api/auth/totp/begin", a.handleTOTPBegin)
	r.Post("/api/auth/totp/verify", a.handleTOTPVerify)
	r.Post("/api/auth/totp/login", a.handleTOTPLogin)
	r.Post("/api/auth/totp/disable", a.handleTOTPDisable)
	r.Post("/api/auth/totp/recovery", a.handleTOTPRecovery)
	r.Get("/api/auth/oauth/{provider}", a.handleOAuthStart)
	r.Get("/api/auth/oauth/{provider}/callback", a.handleOAuthCallback)
	return r
}

// jsonStatusKey is the envelope key for trivial {"status":"ok"} bodies.
const jsonStatusKey = "status"

// maxAuthBody bounds auth request bodies (1 MiB). Passwords and codes are
// bytes, never megabytes; without a cap a huge body is a memory sink
// before validation even runs. Argon2's cost is ~constant in input length,
// so this bounds transport, not hashing.
const maxAuthBody = 1 << 20

func readJSON(r *http.Request, dst any) error {
	r.Body = http.MaxBytesReader(nil, r.Body, maxAuthBody)
	defer r.Body.Close()
	dec := json.NewDecoder(r.Body)
	dec.DisallowUnknownFields()
	return dec.Decode(dst)
}

func writeJSON(w http.ResponseWriter, v any) {
	w.Header().Set("Content-Type", "application/json")
	w.WriteHeader(http.StatusOK)
	_ = json.NewEncoder(w).Encode(v)
}

func (a *Auth) handleSignup(w http.ResponseWriter, r *http.Request) {
	if a.signupClosed() {
		http.Error(w, "signup closed", http.StatusForbidden)
		return
	}
	if !a.signupAttempts.allow(signinIP(r)) {
		a.secReject(r.Context(), evSignupRejected, reasonSignupThrottled, "", "")
		http.Error(w, "too many attempts", http.StatusTooManyRequests)
		return
	}
	if a.emailVerificationRequired() && (a.publicBaseURL() == "" || a.mailerFn() == nil) {
		http.Error(w, "verification unavailable", http.StatusServiceUnavailable)
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
	if a.emailVerificationRequired() {
		// The signup session is a credential too: revoke it, then send the
		// verification link. The public verify endpoint needs no session.
		_ = a.RevokeAllSessions(r.Context(), user.ID)
		if a.publicBaseURL() == "" || a.mailerFn() == nil {
			http.Error(w, "verification unavailable", http.StatusServiceUnavailable)
			return
		}
		if verr := a.RequestEmailVerification(r.Context(), user.ID, user.Email, a.publicBaseURL()); verr != nil {
			http.Error(w, "verification failed", http.StatusInternalServerError)
			return
		}
		writeJSON(w, map[string]string{jsonStatusKey: "verification_sent"})
		return
	}
	http.SetCookie(w, a.sessionCookie(token))
	writeJSON(w, map[string]string{"email": user.Email})
}

func (a *Auth) handleSignin(w http.ResponseWriter, r *http.Request) {
	var body struct {
		Credential string `json:"credential"`
		Password   string `json:"password"`
	}
	if err := readJSON(r, &body); err != nil {
		http.Error(w, "invalid request", http.StatusBadRequest)
		return
	}
	// Per-IP throttle BEFORE the argon check: bogus logins cost ~250ms
	// of hashing each, so an unthrottled endpoint is CPU-DoS by design.
	// Same 401 shape when throttled (no oracle distinguishing "blocked"
	// from "wrong"). Generous budget (stuffing scripts do thousands;
	// offices do dozens), per-IP keyed (never per-email: tracking by
	// email would let anyone lock anyone else out). X-Forwarded-For is
	// deliberately NOT trusted — behind a proxy, throttle there; trusting
	// a client-controlled header here would let attackers rotate past it.
	ip := signinIP(r)
	if !a.signinAttempts.allow(ip) {
		a.secReject(r.Context(), evLoginRejected, reasonSigninThrottled, body.Credential, "")
		http.Error(w, "invalid credentials", http.StatusUnauthorized)
		return
	}
	token, totpRequired, err := a.Login(r.Context(), body.Credential, body.Password)
	if err != nil {
		if errors.Is(err, ErrBadCredentials) {
			http.Error(w, "invalid credentials", http.StatusUnauthorized)
			return
		}
		if errors.Is(err, ErrEmailUnverified) {
			http.Error(w, "email verification required", http.StatusForbidden)
			return
		}
		http.Error(w, "login failed", http.StatusInternalServerError)
		return
	}
	// Password verified (session opened or TOTP pending): the failures
	// allow() recorded above were typos, not an attack — forget them.
	a.signinAttempts.forget(ip)
	if totpRequired {
		// No session yet: the client prompts for the authenticator code
		// and completes at /api/auth/totp/login with its user id.
		addr, nerr := normalizeEmail(body.Credential)
		if nerr != nil {
			http.Error(w, "login failed", http.StatusInternalServerError)
			return
		}
		user, lerr := a.q.GetUserByEmail(r.Context(), addr)
		if lerr != nil {
			http.Error(w, "login failed", http.StatusInternalServerError)
			return
		}
		writeJSON(w, map[string]any{"totp_required": true, "userId": user.ID})
		return
	}
	http.SetCookie(w, a.sessionCookie(token))
	writeJSON(w, map[string]string{jsonStatusKey: "ok"})
}

func (a *Auth) handleSignout(w http.ResponseWriter, r *http.Request) {
	if cookie, err := r.Cookie(CookieName); err == nil {
		a.Logout(r.Context(), cookie.Value)
	}
	a.clearCookie(w)
	writeJSON(w, map[string]string{jsonStatusKey: "ok"})
}
