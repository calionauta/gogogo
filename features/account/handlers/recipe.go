// The account page declares its surface to the recipe seam.
package handlers

import (
	"github.com/go-chi/chi/v5"

	"github.com/calionauta/gogogo/internal/recipe"
)

// RecipeID is the recipe id the account unit declares itself under.
const RecipeID = "account"

// Recipe returns the declaration the composition root mounts through the
// recipe seam.
//
// The unit mounts later than its neighbours only because the root built it
// after composing the tree. Building it from the same dependency bundle the
// root already has removes that ordering exception, so the declaration lives
// beside its routes like every other converted unit.
func (h *Handler) Recipe() recipe.Provider { return recipeProvider{h: h} }

type recipeProvider struct{ h *Handler }

func (p recipeProvider) ID() string { return RecipeID }

func (p recipeProvider) Declaration() recipe.Declaration {
	const (
		get  = "GET"
		post = "POST"
	)
	return recipe.Declaration{
		Routes: []recipe.Route{
			{Method: get, Pattern: "/account"},
			{Method: post, Pattern: "/account/password"},
			{Method: post, Pattern: "/account/verify"},
			{Method: post, Pattern: "/account/totp/begin"},
			{Method: post, Pattern: "/account/totp/verify"},
			{Method: post, Pattern: "/account/totp/disable"},
			{Method: post, Pattern: "/account/totp/recovery"},
			{Method: post, Pattern: "/account/sessions/revoke"},
			{Method: post, Pattern: "/account/sessions/revoke-all"},
			{Method: post, Pattern: "/account/cloud"},
			{Method: post, Pattern: "/account/cloud/disconnect"},
			{Method: post, Pattern: "/account/device-token"},
		},
		// Only the two mutations that need no operator-held secret become
		// verbs: re-sending a verification mail, and revoking one session by
		// id (still owner-scoped by the handler).
		Actions: []recipe.InvokeAction{
			{
				Verb:        "resend_verification",
				Description: "Send the caller another email-verification link.",
				Route:       recipe.Route{Method: post, Pattern: "/account/verify"},
			},
			{
				Verb:        "revoke_session",
				Description: "Revoke one of the caller's own sessions by id.",
				Route:       recipe.Route{Method: post, Pattern: "/account/sessions/revoke"},
				Params:      []string{"session_id"},
				ParamHelp:   map[string]string{"session_id": "The session id shown on the account page."},
			},
			{
				Verb:        "disconnect_cloud",
				Description: "Forget the saved cloud sync URL and token; takes effect on the next boot.",
				Route:       recipe.Route{Method: post, Pattern: "/account/cloud/disconnect"},
			},
		},
		// The rest are the security invariant, not an oversight: a credential
		// (current password, TOTP code, sync token, one-time bearer) must come
		// from the operator, never from the caller of an invoke. Step-up is the
		// whole point of those routes, so a headless caller has no honest way to
		// satisfy it.
		Omitted: []recipe.OmittedRoute{
			{
				Route:  recipe.Route{Method: post, Pattern: "/account/password"},
				Reason: "step-up: requires the current password, a credential a headless caller must not hold",
			},
			{
				Route: recipe.Route{Method: post, Pattern: "/account/totp/begin"},
				Reason: "answers with the enrollment page and a fresh shared secret; " +
					"reading it back headless leaks the second factor",
			},
			{
				Route:  recipe.Route{Method: post, Pattern: "/account/totp/verify"},
				Reason: "needs the authenticator's one-time code, a second factor the caller must not hold",
			},
			{
				Route:  recipe.Route{Method: post, Pattern: "/account/totp/disable"},
				Reason: "step-up: disabling the second factor requires a fresh code or password",
			},
			{
				Route:  recipe.Route{Method: post, Pattern: "/account/totp/recovery"},
				Reason: "mints one-time recovery codes and shows them once; a headless caller must not receive them",
			},
			{
				Route:  recipe.Route{Method: post, Pattern: "/account/sessions/revoke-all"},
				Reason: "step-up: sign-out-everywhere requires the password or a recovery code",
			},
			{
				Route:  recipe.Route{Method: post, Pattern: "/account/cloud"},
				Reason: "requires the operator's cloud sync token, a secret that must come from the operator",
			},
			{
				Route: recipe.Route{Method: post, Pattern: "/account/device-token"},
				Reason: "mints a one-time bearer device token and renders it; a headless " +
					"caller must not receive a bearer credential",
			},
		},
	}
}

func (p recipeProvider) Routes() chi.Router { return p.h.Routes() }
