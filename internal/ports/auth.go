// SCOPE:feature,removal=plugin - Auth service interface (POC)
package ports

import "net/http"

// AuthService abstracts auth operations while preserving cookie compatibility.
type AuthService interface {
	IssueCookie(w http.ResponseWriter, userID string) error
	ClearCookie(w http.ResponseWriter)
	LoadFromRequest(r *http.Request) (userID string, ok bool)
}
