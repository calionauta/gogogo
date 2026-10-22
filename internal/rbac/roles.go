// SCOPE:core — RBAC role vocabulary (single home for LLM discovery).
//
// Role lifecycle, end to end:
//  1. config (ADMIN_EMAILS / VIEWER_EMAILS) names addresses,
//  2. internal/server/boot_helpers.go promoteRole writes users.role,
//  3. subjectOf below resolves a session to a decision subject,
//  4. casbin/policy.csv decides subject + path + method.
//
// Enforcement lives ONLY in the policy. IsViewer is a display hint:
// a wrong answer degrades to a 403, never to an unauthorized write.
package rbac

import (
	"github.com/calionauta/gogogo/internal/auth"
)

// Decision subjects. Unknown users.role values fall back to RoleUser
// (least surprise), never up: only the exact strings below leave the
// default, and viewer can only lose permissions, never gain.
const (
	// RoleAdmin promotes via ADMIN_EMAILS; the policy allows admin on /*.
	RoleAdmin = "admin"
	// RoleUser is any other authenticated user (unknown values included).
	RoleUser = "user"
	// RoleViewer is the restricted demo role (VIEWER_EMAILS): read +
	// create + toggle on todos, no deletes, no paid actions.
	RoleViewer = "viewer"
	// RoleAnonymous is requests without a valid session.
	RoleAnonymous = "anonymous"
)

// IsViewer reports whether a session role gets the restricted demo
// surface (disabled delete affordances + message instead of modal).
func IsViewer(role string) bool {
	return role == RoleViewer
}

// IsAdmin reports whether a session role is the admin role. A display hint, like
// IsViewer: the shell uses it to hide the admin-only module from the sidebar, and
// the route stays policy-enforced. A wrong answer costs a 403 on click, never an
// unauthorized write — and the comparison lives here because a role comparison
// anywhere else is a routing decision wearing a boolean.
func IsAdmin(role string) bool {
	return role == RoleAdmin
}

// subjectOf resolves the decision subject: the session's role, "user"
// for anything unrecognized, "anonymous" without a session. Unknown
// values must not lock users out over a typo in the column — and must
// never escalate: only the exact strings "admin" and "viewer" leave
// the user default, and viewer can only lose permissions, never gain.
//
// Why no role inheritance (no `g, viewer, user` line): Casbin
// inheritance only ADDS permissions. Viewer is a SUBSET of user, so
// inheriting from user would escalate every viewer to full user.
// The three roles stay flat on purpose; the subset is expressed by
// enumerating viewer paths in policy.csv, not by hierarchy.
func subjectOf(sess *auth.ValidatedSession, err error) string {
	if err != nil || sess == nil {
		return RoleAnonymous
	}
	if sess.Role == RoleAdmin {
		return RoleAdmin
	}
	if sess.Role == RoleViewer {
		return RoleViewer
	}
	return RoleUser
}
