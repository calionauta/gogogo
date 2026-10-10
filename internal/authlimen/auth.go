// SCOPE:feature,removal=plugin - limen-backed auth service (POC)
package authlimen

import (
	"net/http"
	"time"

	"github.com/calionauta/gogogo/internal/ports"
)

const cookieName = "gogogo_auth"

type Service struct {
	secure bool
}

func New(secure bool) *Service {
	return &Service{secure: secure}
}

func (s *Service) IssueCookie(w http.ResponseWriter, userID string) error {
	http.SetCookie(w, &http.Cookie{
		Name:     cookieName,
		Value:    userID, // POC: simplified; real impl uses signed token
		Path:     "/",
		HttpOnly: true,
		Secure:   s.secure,
		SameSite: http.SameSiteLaxMode,
		Expires:  time.Now().Add(30 * 24 * time.Hour),
	})
	return nil
}

func (s *Service) ClearCookie(w http.ResponseWriter) {
	http.SetCookie(w, &http.Cookie{
		Name:     cookieName,
		Value:    "",
		Path:     "/",
		HttpOnly: true,
		Secure:   s.secure,
		SameSite: http.SameSiteLaxMode,
		MaxAge:   -1,
		Expires:  time.Unix(0, 0),
	})
}

func (s *Service) LoadFromRequest(r *http.Request) (string, bool) {
	c, err := r.Cookie(cookieName)
	if err != nil || c.Value == "" {
		return "", false
	}
	return c.Value, true
}

var _ ports.AuthService = (*Service)(nil)
