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
// State parameters live in a short-lived in-process store (single-box
// deployments; a multi-box fleet behind one remote Turso would need the
// states table-shared — noted, not built, until the fleet exists).
package auth

import (
	"context"
	"crypto/rand"
	"database/sql"
	"encoding/base64"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"net/http"
	"time"

	"golang.org/x/oauth2"

	"github.com/calionauta/gogogo/internal/db"
	dbgen "github.com/calionauta/gogogo/internal/db/sqlcgen"
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
}

// OAuthGoogle returns the preset for Google sign-in (OpenID Connect
// userinfo carries a verified email claim).
func OAuthGoogle(clientID, clientSecret, redirectURL string) OAuthProvider {
	return OAuthProvider{
		// #nosec G101 — ClientSecret here is the caller's parameter, not a
		// hardcoded credential; gosec matches the field name.
		Name: "google", ClientID: clientID, ClientSecret: clientSecret,
		RedirectURL: redirectURL,
		AuthURL:     "https://accounts.google.com/o/oauth2/v2/auth",
		TokenURL:    "https://oauth2.googleapis.com/token",
		UserInfoURL: "https://openidconnect.googleapis.com/v1/userinfo",
		Scopes:      []string{"openid", "email", "profile"},
		ParseEmail:  parseGoogleEmail,
	}
}

// OAuthGitHub returns the preset for GitHub sign-in. GitHub's /user
// payload may omit email (private addresses), so the preset reads
// /user/emails and picks the verified primary — pass that URL and the
// user:email scope.
func OAuthGitHub(clientID, clientSecret, redirectURL string) OAuthProvider {
	return OAuthProvider{
		// #nosec G101 — see OAuthGoogle above: parameter, not a secret.
		Name: "github", ClientID: clientID, ClientSecret: clientSecret,
		RedirectURL: redirectURL,
		AuthURL:     "https://github.com/login/oauth/authorize",
		TokenURL:    "https://github.com/login/oauth/access_token",
		UserInfoURL: "https://api.github.com/user/emails",
		Scopes:      []string{"user:email"},
		ParseEmail:  parseGitHubEmail,
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
	state, verifier, err := a.newOAuthState(provider)
	if err != nil {
		http.Error(w, "login failed", http.StatusInternalServerError)
		return
	}
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
	verifier, ok := a.takeOAuthState(r.URL.Query().Get("state"), provider)
	if !ok {
		http.Error(w, "invalid login state", http.StatusBadRequest)
		return
	}
	tok, err := oauthConfig(p).Exchange(r.Context(), r.URL.Query().Get("code"), oauth2.VerifierOption(verifier))
	if err != nil {
		http.Error(w, "login failed", http.StatusBadGateway)
		return
	}
	email, err := fetchOAuthEmail(r.Context(), p, tok.AccessToken)
	if err != nil {
		http.Error(w, "login failed", http.StatusBadGateway)
		return
	}
	user, err := a.findOrCreateOAuthUser(r.Context(), provider, p, email)
	if err != nil {
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

// fetchOAuthEmail GETs the provider's userinfo with the access token.
func fetchOAuthEmail(ctx context.Context, p OAuthProvider, accessToken string) (string, error) {
	req, err := http.NewRequestWithContext(ctx, http.MethodGet, p.UserInfoURL, nil)
	if err != nil {
		return "", err
	}
	req.Header.Set("Authorization", "Bearer "+accessToken)
	resp, err := http.DefaultClient.Do(req)
	if err != nil {
		return "", err
	}
	defer resp.Body.Close()
	if resp.StatusCode != http.StatusOK {
		return "", fmt.Errorf("auth: userinfo status %d", resp.StatusCode)
	}
	body, err := io.ReadAll(io.LimitReader(resp.Body, oauthBodyLimit))
	if err != nil {
		return "", err
	}
	email, err := p.ParseEmail(body)
	if err != nil {
		return "", err
	}
	return normalizeEmail(email)
}

// findOrCreateOAuthUser links (provider, subject) to an app user: reuse
// the link, else reuse the email row, else provision. Subject comes from
// the provider as the stable account id — resolve it before calling.
func (a *Auth) findOrCreateOAuthUser(
	ctx context.Context, provider string, p OAuthProvider, email string,
) (*User, error) {
	subject, err := oauthSubject(p, email)
	if err != nil {
		return nil, err
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
		now := db.Now()
		row, err = a.q.CreateUser(ctx, dbgen.CreateUserParams{
			ID: db.NewID(), Email: email, PasswordHash: "!", Name: "",
			Created: now, Updated: now,
		})
		if err != nil {
			return nil, fmt.Errorf("auth: oauth provision: %w", err)
		}
	}
	now := db.Now()
	if err := a.q.LinkOAuthAccount(ctx, dbgen.LinkOAuthAccountParams{
		Provider: provider, Subject: subject, UserID: row.ID, Created: now,
	}); err != nil {
		return nil, fmt.Errorf("auth: oauth link: %w", err)
	}
	return &User{ID: row.ID, Email: row.Email}, nil
}

// oauthSubject resolves the stable provider-side account id. OAuth 2.0
// itself has no subject claim — each preset documents its source here.
// The default (email as subject) fits providers whose userinfo key is the
// verified email; presets needing more override by wrapping
// findOrCreateOAuthUser, not by special-casing this function.
func oauthSubject(p OAuthProvider, email string) (string, error) {
	if email == "" {
		return "", errors.New("auth: empty oauth subject")
	}
	return p.Name + ":" + email, nil
}
