// SCOPE:feature — Admin user-action role predicates.
//
// Ruleguard allows role comparisons in a feature permissions.go: the
// comparison is a local admin-action guard, not a route policy (the route is
// already fenced in Routes and policy.csv).
package handlers

import "github.com/calionauta/gogogo/internal/rbac"

func isAdminRole(role string) bool {
	return role == rbac.RoleAdmin
}

func validRole(role string) bool {
	switch role {
	case rbac.RoleUser, rbac.RoleViewer, rbac.RoleAdmin:
		return true
	default:
		return false
	}
}
