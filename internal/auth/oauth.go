// SCOPE:core — Social logins over golang.org/x/oauth2
//
// Federated login for providers speaking OAuth 2.0 authorization code flow.
// The only dependency is x/oauth2 itself (Google-maintained, already in the
// module graph): endpoints and email parsing are plain data + functions per
// provider, so adding one is config, not code. Google and GitHub ship as
// presets; anything else fills OAuthProvider directly.
//
// Linking: (provider, subject) rows in oauth_accounts point at app users.
// First login with an address that already exists links rather than
// duplicating — one human, one users row, however many ways in.
//
// Residual, stated plainly: that first-time email match trusts the
// provider's verification. If a provider reassigns an address to a new
// human AND our row still carries it with no link yet, the new human
// links to the old row. Sub-keying cannot prevent that first contact
// (there is no link yet to prefer); it prevents everything after (address
// changes never move an established link). Apps where that residual is
// unacceptable should link only from an authenticated session instead of
// on first contact.
//
// State parameters live in a short-lived in-process store (single-box
// deployments; a multi-box fleet behind one remote Turso would need the
// states table-shared — noted, not built, until the fleet exists).
package auth

import (
	"context"
	"crypto/rand"
	"crypto/subtle"
	"database/sql"
	"encoding/base64"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"net/http"
	"strconv"
	"time"

	"golang.org/x/oauth2"

	dbgen "github.com/calionauta/gogogo/internal/db/sqlcgen"
	"github.com/calionauta/gogogo/internal/dbschema"
)

// oauthStateTTL bounds the login handshake: the callback must arrive
// within ten minutes of the redirect or the state is forgotten.
const oauthStateTTL = 10 * time.Minute

// oauthStateLen is the handshake entropy in bytes (192 bits, unguessable).
const oauthStateLen = 24

// oauthBodyLimit caps userinfo responses (1 MiB — providers send bytes,
// never megabytes; without a cap a rogue endpoint is a memory sink).
const oauthBodyLimit = 1 << 20

// OAuthProvider describes one federated login source. AuthURL/TokenURL go
// straight into oauth2.Config (tests point them at httptest servers);
// UserInfoURL is fetched with the access token and ParseEmail extracts
// the verified address from its body.
type OAuthProvider struct {
	Name         string
	ClientID     string
	ClientSecret string
	RedirectURL  string
	AuthURL      string
	TokenURL     string
	UserInfoURL  string
	Scopes       []string
	ParseEmail   func(body []byte) (string, error)
	// SubjectURL fetches the stable provider-side account id (the "sub"
	// claim). Empty reuses the UserInfoURL response. ParseSubject extracts
	// it and is required: an email address can change hands (deleted
	// accounts get recycled), and a link keyed by email follows the
	// address to its new owner — a quiet account takeover. A sub never does.
	SubjectURL   string
	ParseSubject func(body []byte) (string, error)
}

// OAuthGoogle returns the preset for Google sign-in (OpenID Connect
// userinfo carries a verified email claim AND a stable sub claim — the
// subject the link is keyed by).
func OAuthGoogle(clientID, clientSecret, redirectURL string) OAuthProvider {
	return OAuthProvider{
		// #nosec G101 — ClientSecret here is the caller's parameter, not a
		// hardcoded credential; gosec matches the field name.
		Name: "google", ClientID: clientID, ClientSecret: clientSecret,
		RedirectURL:  redirectURL,
		AuthURL:      "https://accounts.google.com/o/oauth2/v2/auth",
		TokenURL:     "https://oauth2.googleapis.com/token",
		UserInfoURL:  "https://openidconnect.googleapis.com/v1/userinfo",
		Scopes:       []string{"openid", "email", "profile"},
		ParseEmail:   parseGoogleEmail,
		ParseSubject: parseGoogleSubject,
	}
}

// OAuthGitHub returns the preset for GitHub sign-in. GitHub's /user
// payload may omit email (private addresses), so the preset reads
// /user/emails and picks the verified primary — pass that URL and the
// user:email scope. The stable subject is /user's numeric id (never
// reassigned, unlike logins and emails), fetched separately.
func OAuthGitHub(clientID, clientSecret, redirectURL string) OAuthProvider {
	return OAuthProvider{
		// #nosec G101 — see OAuthGoogle above: parameter, not a secret.
		Name: "github", ClientID: clientID, ClientSecret: clientSecret,
		RedirectURL:  redirectURL,
		AuthURL:      "https://github.com/login/oauth/authorize",
		TokenURL:     "https://github.com/login/oauth/access_token",
		UserInfoURL:  "https://api.github.com/user/emails",
		Scopes:       []string{"user:email"},
		ParseEmail:   parseGitHubEmail,
		SubjectURL:   "https://api.github.com/user",
		ParseSubject: parseGitHubSubject,
	}
}

func parseGoogleEmail(body []byte) (string, error) {
	//nolint:tagliatelle // Google's userinfo wire format; renaming would
	// break the external contract, not ours.
	var v struct {
		Email         string `json:"email"`
		EmailVerified bool   `json:"email_verified"`
	}
	if err := json.Unmarshal(body, &v); err != nil {
		return "", fmt.Errorf("auth: google userinfo decode: %w", err)
	}
	if v.Email == "" || !v.EmailVerified {
		return "", errors.New("auth: google email missing or unverified")
	}
	return v.Email, nil
}

func parseGitHubEmail(body []byte) (string, error) {
	var list []struct {
		Email    string `json:"email"`
		Primary  bool   `json:"primary"`
		Verified bool   `json:"verified"`
	}
	if err := json.Unmarshal(body, &list); err != nil {
		return "", fmt.Errorf("auth: github emails decode: %w", err)
	}
	for _, e := range list {
		if e.Primary && e.Verified && e.Email != "" {
			return e.Email, nil
		}
	}
	for _, e := range list {
		if e.Verified && e.Email != "" {
			return e.Email, nil
		}
	}
	return "", errors.New("auth: github verified email not found")
}

// parseGoogleSubject extracts the stable sub claim: the link key, so the
// account survives address changes (and a recycled address cannot take
// over someone else's link).
func parseGoogleSubject(body []byte) (string, error) {
	var v struct {
		Sub string `json:"sub"`
	}
	if err := json.Unmarshal(body, &v); err != nil {
		return "", fmt.Errorf("auth: google subject decode: %w", err)
	}
	if v.Sub == "" {
		return "", errors.New("auth: google sub missing")
	}
	return v.Sub, nil
}

// parseGitHubSubject extracts /user's numeric id: stable across login and
// email changes (both get recycled; ids never are).
func parseGitHubSubject(body []byte) (string, error) {
	var v struct {
		ID int64 `json:"id"`
	}
	if err := json.Unmarshal(body, &v); err != nil {
		return "", fmt.Errorf("auth: github subject decode: %w", err)
	}
	if v.ID == 0 {
		return "", errors.New("auth: github id missing")
	}
	return strconv.FormatInt(v.ID, 10), nil
}

// oauthState is one in-flight handshake. The PKCE verifier binds the
// callback's code exchange to the browser that started it: a leaked
// authorization code alone cannot be redeemed (RFC 7636, recommended for
// every OAuth client in 2026, not just public ones).
type oauthState struct {
	provider string
	verifier string
	expires  time.Time
}

// SetOAuthProviders installs the federated sources. Empty (the default)
// disables the oauth routes: unknown providers 404.
func (a *Auth) SetOAuthProviders(providers map[string]OAuthProvider) {
	a.mu.Lock()
	defer a.mu.Unlock()
	a.oauth = providers
}

func (a *Auth) oauthProvider(name string) (OAuthProvider, bool) {
	a.mu.Lock()
	defer a.mu.Unlock()
	p, ok := a.oauth[name]
	return p, ok
}

// oauthConfig builds the x/oauth2 config for a provider. Endpoints are
// data, so tests inject httptest URLs without touching this code.
func oauthConfig(p OAuthProvider) *oauth2.Config {
	return &oauth2.Config{
		ClientID:     p.ClientID,
		ClientSecret: p.ClientSecret,
		RedirectURL:  p.RedirectURL,
		Scopes:       p.Scopes,
		Endpoint: oauth2.Endpoint{
			AuthURL:  p.AuthURL,
			TokenURL: p.TokenURL,
		},
	}
}

// oauthStateMax bounds the in-process handshake map. Each entry expires in
// ten minutes; without a cap, a public start endpoint is a memory sink.
const oauthStateMax = 4096

// oauthBindCookie binds a handshake to the browser that started it. The
// state is already unguessable, but without a browser-bound copy a leaked
// callback URL can be opened by another browser and turn into a login-CSRF
// (the victim lands in the attacker's account). Path-scoped to the provider
// subtree.
const oauthBindCookie = "gogogo_oauth_bind"

// newOAuthState mints an unguessable handshake id and remembers it.
func (a *Auth) newOAuthState(provider string) (state, verifier string, err error) {
	raw := make([]byte, oauthStateLen)
	if _, err := rand.Read(raw); err != nil {
		return "", "", fmt.Errorf("auth: oauth state entropy: %w", err)
	}
	state = base64.RawURLEncoding.EncodeToString(raw)
	verifier = oauth2.GenerateVerifier()
	a.mu.Lock()
	defer a.mu.Unlock()
	if a.states == nil {
		a.states = map[string]oauthState{}
	}
	now := time.Now()
	for k, v := range a.states {
		if now.After(v.expires) {
			delete(a.states, k)
		}
	}
	if len(a.states) >= oauthStateMax {
		return "", "", errors.New("auth: oauth state capacity")
	}
	a.states[state] = oauthState{provider: provider, verifier: verifier, expires: now.Add(oauthStateTTL)}
	return state, verifier, nil
}

// takeOAuthState consumes a handshake id (single-use, like magic tokens),
// returning its PKCE verifier for the code exchange.
func (a *Auth) takeOAuthState(state, provider string) (string, bool) {
	a.mu.Lock()
	defer a.mu.Unlock()
	st, ok := a.states[state]
	if !ok || st.provider != provider || time.Now().After(st.expires) {
		delete(a.states, state)
		return "", false
	}
	delete(a.states, state)
	return st.verifier, true
}

// OAuthStart redirects to the provider. Unknown providers 404 (not 500:
// absence of configuration is a client error here, not a failure).
func (a *Auth) OAuthStart(w http.ResponseWriter, r *http.Request, provider string) {
	p, ok := a.oauthProvider(provider)
	if !ok {
		http.NotFound(w, r)
		return
	}
	if !a.oauthStartAttempts.allow(signinIP(r)) {
		http.Error(w, "too many attempts", http.StatusTooManyRequests)
		return
	}
	state, verifier, err := a.newOAuthState(provider)
	if err != nil {
		http.Error(w, "login failed", http.StatusInternalServerError)
		return
	}
	// #nosec G124 — Secure follows the environment; HttpOnly and SameSite set.
	http.SetCookie(w, &http.Cookie{
		Name:     oauthBindCookie,
		Value:    state,
		Path:     "/api/auth/oauth",
		MaxAge:   int(oauthStateTTL.Seconds()),
		HttpOnly: true,
		Secure:   a.secure,
		SameSite: http.SameSiteLaxMode,
	})
	http.Redirect(w, r, oauthConfig(p).AuthCodeURL(state,
		oauth2.SetAuthURLParam("code_challenge", oauth2.S256ChallengeFromVerifier(verifier)),
		oauth2.SetAuthURLParam("code_challenge_method", "S256"),
	), http.StatusFound)
}

// OAuthCallback completes the handshake: state check, code exchange,
// email fetch, find-or-create + link, session cookie, redirect home.
func (a *Auth) OAuthCallback(w http.ResponseWriter, r *http.Request, provider string) {
	p, ok := a.oauthProvider(provider)
	if !ok {
		http.NotFound(w, r)
		return
	}
	state := r.URL.Query().Get("state")
	cookie, cerr := r.Cookie(oauthBindCookie)
	if state == "" || cerr != nil ||
		subtle.ConstantTimeCompare([]byte(cookie.Value), []byte(state)) != 1 {
		http.Error(w, "invalid login state", http.StatusBadRequest)
		return
	}
	// #nosec G124 — clearing cookie; Secure follows the environment.
	http.SetCookie(w, &http.Cookie{
		Name: oauthBindCookie, Value: "", Path: "/api/auth/oauth", MaxAge: -1,
		HttpOnly: true, Secure: a.secure, SameSite: http.SameSiteLaxMode,
	})
	verifier, ok := a.takeOAuthState(state, provider)
	if !ok {
		http.Error(w, "invalid login state", http.StatusBadRequest)
		return
	}
	tok, err := oauthConfig(p).Exchange(r.Context(), r.URL.Query().Get("code"), oauth2.VerifierOption(verifier))
	if err != nil {
		http.Error(w, "login failed", http.StatusBadGateway)
		return
	}
	email, subject, err := fetchOAuthIdentity(r.Context(), p, tok.AccessToken)
	if err != nil {
		http.Error(w, "login failed", http.StatusBadGateway)
		return
	}
	user, err := a.findOrCreateOAuthUser(r.Context(), provider, email, subject)
	if err != nil {
		if errors.Is(err, ErrSignupClosed) {
			http.Error(w, "signup closed", http.StatusForbidden)
			return
		}
		http.Error(w, "login failed", http.StatusInternalServerError)
		return
	}
	token, err := a.openSession(r.Context(), user.ID)
	if err != nil {
		http.Error(w, "login failed", http.StatusInternalServerError)
		return
	}
	http.SetCookie(w, a.sessionCookie(token))
	http.Redirect(w, r, "/todo", http.StatusSeeOther)
}

// fetchOAuthIdentity resolves the verified email and the stable subject,
// namespaced by provider.
func fetchOAuthIdentity(ctx context.Context, p OAuthProvider, accessToken string) (email, subject string, err error) {
	body, err := fetchOAuthURL(ctx, p.UserInfoURL, accessToken)
	if err != nil {
		return "", "", err
	}
	email, err = p.ParseEmail(body)
	if err != nil {
		return "", "", err
	}
	email, err = normalizeEmail(email)
	if err != nil {
		return "", "", err
	}
	if p.ParseSubject == nil {
		return "", "", errors.New("auth: provider has no subject extractor")
	}
	subURL := p.SubjectURL
	subBody := body
	if subURL != "" && subURL != p.UserInfoURL {
		subBody, err = fetchOAuthURL(ctx, subURL, accessToken)
		if err != nil {
			return "", "", err
		}
	}
	sub, err := p.ParseSubject(subBody)
	if err != nil {
		return "", "", err
	}
	return email, p.Name + ":" + sub, nil
}

// fetchOAuthURL GETs one provider URL with the access token, capped.
func fetchOAuthURL(ctx context.Context, url, accessToken string) ([]byte, error) {
	req, err := http.NewRequestWithContext(ctx, http.MethodGet, url, nil)
	if err != nil {
		return nil, err
	}
	req.Header.Set("Authorization", "Bearer "+accessToken)
	resp, err := http.DefaultClient.Do(req)
	if err != nil {
		return nil, err
	}
	defer resp.Body.Close()
	if resp.StatusCode != http.StatusOK {
		return nil, fmt.Errorf("auth: userinfo status %d", resp.StatusCode)
	}
	body, err := io.ReadAll(io.LimitReader(resp.Body, oauthBodyLimit))
	if err != nil {
		return nil, err
	}
	return body, nil
}

// findOrCreateOAuthUser links (provider, subject) to an app user: reuse
// the link, else reuse the email row, else provision. One human, one users
// row, however many ways in. Subjects are stable provider ids; email rows
// are matched only on first contact (see the linking comment above).
func (a *Auth) findOrCreateOAuthUser(
	ctx context.Context, provider, email, subject string,
) (*User, error) {
	if subject == "" {
		return nil, errors.New("auth: empty oauth subject")
	}
	link, lerr := a.q.GetOAuthAccount(ctx, dbgen.GetOAuthAccountParams{
		Provider: provider, Subject: subject,
	})
	if lerr == nil {
		user, uerr := a.q.GetUser(ctx, link.UserID)
		if uerr != nil {
			return nil, fmt.Errorf("auth: linked user gone: %w", uerr)
		}
		return &User{ID: user.ID, Email: user.Email}, nil
	}
	row, err := a.q.GetUserByEmail(ctx, email)
	if err != nil {
		if !errors.Is(err, sql.ErrNoRows) {
			return nil, fmt.Errorf("auth: oauth lookup: %w", err)
		}
		if a.signupClosed() {
			return nil, ErrSignupClosed
		}
		now := dbschema.Now()
		row, err = a.q.CreateUser(ctx, dbgen.CreateUserParams{
			ID: dbschema.NewID(), Email: email, PasswordHash: "!", Name: "",
			Created: now, Updated: now,
		})
		if err != nil {
			return nil, fmt.Errorf("auth: oauth provision: %w", err)
		}
	}
	now := dbschema.Now()
	if err := a.q.LinkOAuthAccount(ctx, dbgen.LinkOAuthAccountParams{
		Provider: provider, Subject: subject, UserID: row.ID, Created: now,
	}); err != nil {
		return nil, fmt.Errorf("auth: oauth link: %w", err)
	}
	// The provider already verified this address (fetchOAuthIdentity
	// rejects unverified emails), so it satisfies the verification gate.
	if verr := a.q.SetEmailVerified(ctx, dbgen.SetEmailVerifiedParams{
		EmailVerifiedAt: now, Updated: now, ID: row.ID,
	}); verr != nil {
		return nil, fmt.Errorf("auth: oauth verify: %w", verr)
	}
	return &User{ID: row.ID, Email: row.Email}, nil
}
