// The auth unit's declaration: it is an Authorization Server, not a recipe.
//
// Why this unit declares no product routes. Every other unit says "here is a
// surface a caller may act on", and the headless rule asks each mutating route
// to name the verb an agent calls or the reason it cannot. Auth is the one unit
// where that question does not apply: it ISSUES the credential the caller
// arrives with. `signin`, `signup`, `password`, `totp/*`, `oauth/*` and
// `magic/*` are the grant machinery itself.
//
//	draft-klrc-aiagent-auth-02: "The use of OAuth negates the need for the
//	Agent to have access to a User's credentials when accessing a resource on
//	the User's behalf." Static API keys are named an antipattern for agent
//	identity; MCP 2026-07-28 casts the client as an OAuth 2.1 client and gives
//	user interaction to the authorization server.
//
// So an agent does not sign in here. It arrives holding a bearer this unit
// minted — a `gogogo_auth` session today, resolved at the edge — and the grant
// routes below are how that bearer came to exist, not product actions.
//
// What an agent MAY do is inspect and revoke its OWN session. Those are declared
// as real verbs rather than omissions: declaring them omitted would forbid what
// the standard model allows.
package auth

import (
	"github.com/go-chi/chi/v5"

	"github.com/calionauta/gogogo/internal/recipe"
)

// AuthRecipeID is the id this unit declares itself under, matching the
// capability registry row.
const AuthRecipeID = "auth"

// RoleAuthServer is the one role a declaration can carry that is not a product
// surface. It is data rather than a comment so the mount seam can require a
// Provider from every unit without pretending auth has product routes.
const RoleAuthServer = "authorization-server"

// Recipe returns the declaration the composition root mounts through the recipe
// seam. A nil receiver is a valid, disabled auth: the seam treats a nil Provider
// as "no code, no declaration".
func (a *Auth) Recipe() recipe.Provider {
	if a == nil {
		return nil
	}
	return authProvider{a: a}
}

type authProvider struct{ a *Auth }

func (p authProvider) ID() string { return AuthRecipeID }

// Roles is the seam's channel for "this unit is infrastructure, not a product
// surface". A Provider that names none is mounted and graded like any other; a
// Provider carrying RoleAuthServer issues the credential, and the headless rule
// has nothing to ask of it.
func (p authProvider) Roles() []string { return []string{RoleAuthServer} }

// Declaration claims no Routes on purpose — see the package comment.
//
// The three session verbs are the exception, and they are the reason this
// declaration is not empty: reading your own sessions and signing your own
// bearer out are things an agent genuinely does, and a capability an agent may
// exercise must be declared rather than inferred.
func (p authProvider) Declaration() recipe.Declaration {
	const (
		get  = "GET"
		post = "POST"
	)
	return recipe.Declaration{
		Actions: []recipe.InvokeAction{
			{
				Verb: "read_sessions",
				Description: "Read the caller's own sign-in sessions: device label, " +
					"last use and expiry. Never another account's.",
				Route:       recipe.Route{Method: get, Pattern: "/api/auth/sessions"},
				BrowserPath: "/account",
			},
			{
				Verb: "signout",
				Description: "Sign the caller's own current session out. Revoking " +
					"another account's session is an operator action, not an agent's.",
				Route:       recipe.Route{Method: post, Pattern: "/api/auth/signout"},
				BrowserPath: "/account",
			},
		},
		Omitted: []recipe.OmittedRoute{
			{
				Route: recipe.Route{Method: post, Pattern: "/api/auth/sessions/revoke-all"},
				Reason: "sign-out-everywhere is step-up: it requires the current password " +
					"or a recovery code, a credential a headless caller must not hold. " +
					"The agent-shaped equivalent is the operator revoking it in /account.",
			},
		},
	}
}

// Routes is the grant machinery plus the session verbs. It is mounted as the
// Authorization Server: the seam knows it by role, not by route list.
func (p authProvider) Routes() chi.Router { return p.a.Routes() }
