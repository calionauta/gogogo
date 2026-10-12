// SCOPE:core — Flat auth HTTP surface (signup/signin/signout)
package auth

import (
	"encoding/json"
	"errors"
	"net/http"
)

// Handler serves the auth API under /api/auth (mount at /): signup,
// signin and signout with the same paths and JSON shapes as before, plus
// magic-link, TOTP second factor and OAuth entry points.
func (a *Auth) Handler() http.Handler {
	mux := http.NewServeMux()
	mux.HandleFunc("/api/auth/signup/credential", a.handleSignup)
	mux.HandleFunc("/api/auth/signin/credential", a.handleSignin)
	mux.HandleFunc("/api/auth/signout", a.handleSignout)
	mux.HandleFunc("/api/auth/magic/request", a.handleMagicRequest)
	mux.HandleFunc("/api/auth/magic", a.handleMagicVerify)
	mux.HandleFunc("/api/auth/totp/begin", a.handleTOTPBegin)
	mux.HandleFunc("/api/auth/totp/verify", a.handleTOTPVerify)
	mux.HandleFunc("/api/auth/totp/login", a.handleTOTPLogin)
	mux.HandleFunc("/api/auth/totp/disable", a.handleTOTPDisable)
	mux.HandleFunc("/api/auth/totp/recovery", a.handleTOTPRecovery)
	mux.HandleFunc("/api/auth/oauth/", a.handleOAuth)
	return mux
}

// SessionProbeHandler serves the shared session banner's poll as a
// standalone handler: buildChiMux mounts it at the top-level /api/session
// path (NOT under /api/auth/ — the auth sub-mux would prefix it into
// /api/auth/api/session, a silent 404 the literal-scanning route gate
// cannot see).
func (a *Auth) SessionProbeHandler() http.Handler {
	return http.HandlerFunc(a.handleSessionProbe)
}

// handleSessionProbe answers {"authed": bool} for the current request.
// A missing, unknown or expired cookie reports false (the banner shows
// "session expired"); it never 401s — the probe is a poll, not a gate.
func (a *Auth) handleSessionProbe(w http.ResponseWriter, r *http.Request) {
	authed := false
	if _, err := a.Session(r); err == nil {
		authed = true
	}
	writeJSON(w, map[string]bool{"authed": authed})
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
	writeJSON(w, map[string]string{"email": user.Email})
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
	token, totpRequired, err := a.Login(r.Context(), body.Credential, body.Password)
	if err != nil {
		if errors.Is(err, ErrBadCredentials) {
			http.Error(w, "invalid credentials", http.StatusUnauthorized)
			return
		}
		http.Error(w, "login failed", http.StatusInternalServerError)
		return
	}
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
	if r.Method != http.MethodPost {
		http.Error(w, "method not allowed", http.StatusMethodNotAllowed)
		return
	}
	if cookie, err := r.Cookie(CookieName); err == nil {
		a.Logout(r.Context(), cookie.Value)
	}
	a.clearCookie(w)
	writeJSON(w, map[string]string{jsonStatusKey: "ok"})
}
