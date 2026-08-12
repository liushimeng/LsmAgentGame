// Package api — auth_helpers.go
//
// Small shared types for the admin handlers so tests can inject a stub
// instead of wiring a real *service.UserService (which needs gormDB and the
// full service init path).
//
// UserRoleChecker is the minimum surface the admin handlers need to
// authorize a request — it's intentionally narrower than *service.UserService
// so unit tests can supply a one-method stub. *service.UserService satisfies
// it via its existing GetUserType method.
package api

import (
	"context"

	"LsmAgentGame/models"
)

// UserRoleChecker is the auth-side surface every admin handler depends on.
// Returns the caller's role and a coded error if the lookup failed.
//
// Implemented by *service.UserService — declared here (not in service) so
// handlers can pass a small test stub without dragging in gormDB.
type UserRoleChecker interface {
	GetUserType(ctx context.Context, userID string) (models.UserType, error)
}
