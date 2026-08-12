package middleware

import (
	"strings"

	"LsmWebGame/config"
	"LsmWebGame/errcode"
	"LsmWebGame/util"

	"github.com/gin-gonic/gin"
)

// AuthRequired validates the Bearer token and stashes the user ID in context.
// Use on routes that need an authenticated user.
func AuthRequired(cfg *config.Config) gin.HandlerFunc {
	return func(c *gin.Context) {
		raw := c.GetHeader("Authorization")
		if raw == "" {
			respondAuthErr(c, errcode.ErrAuthMissingToken)
			return
		}
		parts := strings.SplitN(raw, " ", 2)
		if len(parts) != 2 || !strings.EqualFold(parts[0], "Bearer") {
			respondAuthErr(c, errcode.ErrAuthInvalidToken)
			return
		}
		uid, err := util.ParseToken(parts[1], cfg.JWT.Secret)
		if err != nil {
			respondAuthErr(c, errcode.AsError(err).Code)
			return
		}
		c.Set("user_id", uid)
		c.Next()
	}
}

func respondAuthErr(c *gin.Context, code int) {
	c.AbortWithStatusJSON(401, gin.H{
		"code":    code,
		"message": errcode.DefaultMessages[code],
	})
}
