// Package api — auth_helpers_test.go.
//
// Stubs and helpers shared across the model admin / log / wallet test files.
// Kept in the package's _test.go scope so the stub type never leaks into the
// production binary.
package api

import (
	"context"

	"LsmWebGame/errcode"
	"LsmWebGame/models"

	"github.com/gin-gonic/gin"
)

// stubAuthChecker is a one-method implementation of UserRoleChecker that
// returns the role configured at construction time. nil error ⇒ return the
// role. configuredErr ⇒ return (0, err) so tests can verify the
// "lookup failed" branch.
type stubAuthChecker struct {
	role         models.UserType
	err          error
	roleByUID    map[string]models.UserType
	allowDefault models.UserType // used when roleByUID misses
}

func (s *stubAuthChecker) GetUserType(_ context.Context, uid string) (models.UserType, error) {
	if s.err != nil {
		return 0, s.err
	}
	if s.roleByUID != nil {
		if r, ok := s.roleByUID[uid]; ok {
			return r, nil
		}
		if s.allowDefault != 0 {
			return s.allowDefault, nil
		}
		return 0, errcode.Code(errcode.ErrAuthAccountNotFound)
	}
	return s.role, nil
}

// authCtx mimics what middleware.AuthRequired puts on the Gin context. The
// role argument is ignored at the middleware layer — it's only used by the
// stub to decide what UserRoleChecker returns. Kept as the integer form for
// parity with the existing tests.
func authCtx(uid string, role int) gin.HandlerFunc {
	return func(c *gin.Context) {
		c.Set("user_id", uid)
		c.Set("user_type", role)
		c.Next()
	}
}
